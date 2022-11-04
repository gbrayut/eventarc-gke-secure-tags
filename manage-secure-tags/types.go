package main

// PubSubMessage is the payload of a Pub/Sub event.
// See the documentation for more details:
// https://cloud.google.com/pubsub/docs/reference/rest/v1/PubsubMessage
type PubSubMessage struct {
	Message struct {
		Data []byte `json:"data,omitempty"`
		ID   string `json:"id"`
	} `json:"message"`
	Subscription string `json:"subscription"`
}

// gceInstance holds information about individual VMs obtained from v1.compute.instances.insert AuditLog CloudEvents
type gceInstance struct {
	insertid   string
	targetLink string
	instanceid string
	zone       string
	projectid  string
	template   string
}
