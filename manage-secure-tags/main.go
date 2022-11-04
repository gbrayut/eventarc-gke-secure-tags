// Cloud Run Server that uses EventArc to process v1.compute.instances.insert CloudEvents
// for new VMs and create Secure Tag bindings for them.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"google.golang.org/genproto/googleapis/cloud/audit"
	"google.golang.org/genproto/googleapis/logging/v2"
	"google.golang.org/protobuf/encoding/protojson"
)

var (
	// Caches zonal clients for calling Google Cloud Resource Manager APIs
	tm TagManager
)

func main() {
	tm = NewTagManager(context.Background())
	log.Print("starting server...")
	http.HandleFunc("/", handlerDefault)
	// Could also use https://cloud.google.com/eventarc/docs/anthosrun/event-receivers
	// to route things correctly based on headers if you have a mux that supports headers
	http.HandleFunc("/v1/compute/instances_pubsub", handlerGCEInstancesPubSub)
	http.HandleFunc("/v1/compute/instances", handlerGCEInstances)

	// Determine port for HTTP service.
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
		log.Printf("defaulting to port %s", port)
	}

	// Start HTTP server.
	log.Printf("listening on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

// handlerDefault used by health checks
func handlerDefault(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "server 200 OK")
}

// handlerGCEInstancesPubSub receives and processes a Pub/Sub message with LogEntry data pushed via EventArc trigger
// see https://cloud.google.com/eventarc/docs/run/pubsub-authenticated
func handlerGCEInstancesPubSub(w http.ResponseWriter, r *http.Request) {
	var e PubSubMessage
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
		http.Error(w, "Bad HTTP Request", http.StatusBadRequest)
		log.Printf("Bad HTTP Request: %v", http.StatusBadRequest)
		return
	}
	// https://cloud.google.com/eventarc/docs/anthosrun/event-receivers
	log.Printf("handlerGCEInstancesPubSub ID:%s from %s", string(r.Header.Get("Ce-Id")), string(r.Header.Get("Ce-Time")))

	// message data should be a LogEntry protocol buffer, so use a common function to process that
	tm.processLogEntrypb(e.Message.Data, w, r)
}

// handlerGCEInstances receives direct AuditLog v1.compute.instances.insert entries pushed via EventArc trigger
// this only works for the project in which Cloud Run service is deployed. Cross-project must use Pub/Sub triggers
// see https://cloud.google.com/eventarc/docs/run/create-trigger-cloud-audit-logs-gcloud
func handlerGCEInstances(w http.ResponseWriter, r *http.Request) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Bad HTTP Request", http.StatusBadRequest)
		log.Printf("handlerGCEInstances: ReadAll: %s", err)
		return
	}
	tm.processLogEntrypb(body, w, r)
}

// processLogEntrypb takes v1.compute.instances.insert CloudEvents from Pub/Sub or Direct EventArc triggers
// and for the Operation.First LogEntry will
func (t *TagManager) processLogEntrypb(pb []byte, w http.ResponseWriter, r *http.Request) {
	//Get LogEntry https://cloud.google.com/logging/docs/reference/v2/rest/v2/LogEntry
	var le logging.LogEntry
	if err := protojson.Unmarshal(pb, &le); err != nil {
		http.Error(w, "Bad HTTP Request", http.StatusBadRequest)
		log.Printf("processLogEntrypb: Unmarshal: %s", err)
		return
	}

	// get anypb https://pkg.go.dev/google.golang.org/protobuf/types/known/anypb
	m, err := le.GetProtoPayload().UnmarshalNew()
	if err != nil {
		http.Error(w, "OK skip GetProtoPayload", http.StatusOK)
		log.Printf("processLogEntrypb: GetProtoPayload: %s", err)
		return
	}
	//Get AuditLog https://cloud.google.com/compute/docs/reference/rest/v1/instances/insert
	var al audit.AuditLog
	// LogEntry protoPayload can technically have different types
	switch m := m.(type) {
	case *audit.AuditLog: // For --event-filters="methodName=v1.compute.instances.insert" trigger it sould be AuditLog
		al = *m
	//case *logging.v1.RequestLog: // This should only be for AppEngine
	default:
		http.Error(w, "OK skip protoPayload typeurl", http.StatusOK)
		log.Printf("processLogEntrypb: unexpected payload: %s", le.GetProtoPayload().TypeUrl)
		return
	}

	// Only process AuditLogs if they are the First (usually two, second being Last)
	if !le.Operation.First {
		http.Error(w, "OK non-first", http.StatusOK)
		log.Printf("processLogEntrypb: skipping non-first log %s for %#v timestamp %s", le.InsertId, al.ResourceName, le.Timestamp.AsTime())
		return
	}

	// Ack event and log some of the VM details
	// TODO: Switch to using Structured Logs https://cloud.google.com/logging/docs/samples/logging-write-log-entry#logging_write_log_entry-go
	http.Error(w, fmt.Sprintf("OK first log %s for %s", le.InsertId, al.ResourceName), http.StatusOK)
	log.Printf("processLogEntrypb: processing first log %s for %s timestamp %s", le.InsertId, al.ResourceName, le.Timestamp.AsTime())
	log.Printf("processLogEntrypb: targetLink %s startTime %s", al.Response.Fields["targetLink"], al.Response.Fields["startTime"])

	// extract details needed for binding tags
	vm := &gceInstance{
		insertid:   le.InsertId,
		targetLink: al.Response.Fields["targetLink"].String(),
		instanceid: le.Resource.Labels["instance_id"],
		zone:       le.Resource.Labels["zone"],
		projectid:  le.Resource.Labels["project_id"],
		template:   "",
	}

	// MIG and GKE Node Pools set additional metadata information in the request
	metadata := al.Request.Fields["metadata"]
	if metadata != nil {
		for _, item := range metadata.GetStructValue().Fields["items"].GetListValue().Values {
			key := item.GetStructValue().Fields["key"].GetStringValue()
			value := item.GetStructValue().Fields["value"].GetStringValue()
			//log.Printf("processLogEntrypb: vm metadata %s:%s", key, value)
			switch key {
			case "instance-template":
				vm.template = value
				return // bail after finding the value(s) we need
			}
		}
	}

	// lookup desired tags for VM (based on template prefix matching or targetLink)
	tags, err := t.GetDesiredTagsForMIG(vm)
	if err != nil {
		log.Printf("GetDesiredTagsForMIG error %s", err)
	}
	// bind tags to VM
	err = t.BindVMSecureTags(vm, tags)
	if err != nil {
		log.Printf("BindVMSecureTags error %s", err)
	}
	log.Printf("done %s", vm.insertid)
}
