package main

import (
	"context"
	"fmt"
	"log"
	"sync"

	cloudresourcemanager "google.golang.org/api/cloudresourcemanager/v3"
	"google.golang.org/api/option"
)

// GetResourceName returns the full ResourceName for the VM
func (vm *gceInstance) GetResourceName() string {
	// Example: //compute.googleapis.com/projects/my-project/zones/us-west1-a/instances/689579460943534750
	return fmt.Sprintf("//compute.googleapis.com/projects/%s/zones/%s/instances/%s", vm.projectid, vm.zone, vm.instanceid)
}

// TagManager holds the Cloud Clients used for calling the Cloud Resource Manager APIs
type TagManager struct {
	ctx             context.Context
	zoneClients     map[string]*cloudresourcemanager.Service
	zoneClientsLock sync.Mutex
}

// NewTagManager creates a TagManager for calling Cloud Resource Manager APIs
func NewTagManager(ctx context.Context) TagManager {
	return TagManager{
		ctx: ctx,
	}
}

// GetDesiredTags returns a slice of tagValues for a given Managed Instance Group
// It uses Secret Manager as the data source, but any database lookup could be used
func (t *TagManager) GetDesiredTagsForMIG(vm *gceInstance) ([]string, error) {
	//TODO: Implement Secret Manager lookup
	return []string{"tagValues/642676120853"}, nil
}

// GetZoneClient creates a cloudresourcemanager.Service using a zonal endpoint (required for Secure Tags Rest API)
func (t *TagManager) GetZoneClient(zone string) (*cloudresourcemanager.Service, error) {
	t.zoneClientsLock.Lock()
	defer t.zoneClientsLock.Unlock()

	// check for existing client
	if c, ok := t.zoneClients[zone]; ok {
		return c, nil
	}

	// create new client https://pkg.go.dev/google.golang.org/api/cloudresourcemanager/v3
	c, err := cloudresourcemanager.NewService(t.ctx, option.WithEndpoint(zone+"-cloudresourcemanager.googleapis.com"))
	if err != nil {
		return nil, fmt.Errorf("Error creating cloudresourcemanager client in %s: %w", zone, err)
	}
        t.zoneClients[zone] = c
	return c, nil
}

// ListVMSecureTags returns the effective tags for a given VM
func (t *TagManager) ListVMSecureTags(vm *gceInstance) error {
	crmService, err := t.GetZoneClient(vm.zone)
	if err != nil {
		return err
	}

	resp, err := crmService.EffectiveTags.List().Parent(vm.GetResourceName()).PageSize(100).Do()
	if err != nil {
		return fmt.Errorf("Error calling EffectiveTags: %w", err)
	}
	for _, v := range resp.EffectiveTags {
		log.Printf("TagBindingsClient tag: %#v", v)
	}
	//TODO: return values instead of just logging them
	return nil
}

// BindVMSecureTags creates new secure tags for the given VM
func (t *TagManager) BindVMSecureTags(vm *gceInstance, tagvalues []string) error {
	crmService, err := t.GetZoneClient(vm.zone)
	if err != nil {
		return fmt.Errorf("Error creating cloudresourcemanager client: %w", err)
	}

	// https://pkg.go.dev/google.golang.org/api/cloudresourcemanager/v3#TagBinding
	tag := &cloudresourcemanager.TagBinding{
		Parent: vm.GetResourceName(),
	}
	for _, v := range tagvalues {
		tag.TagValue = v
		op, err := crmService.TagBindings.Create(tag).Do()
		if err != nil {
			return fmt.Errorf("Error calling TagBindings.Create(): %w", err)
		}
		log.Printf("TagBindings.Create() operation name: %s", op.Name)
		// see results: gcloud alpha resource-manager operations describe rctb.us-west1-a.7889478677166923103 --location us-west1-a

		// TODO: add to pubsub topic to ensure operation completes without error?
		// via https://pkg.go.dev/google.golang.org/api/cloudresourcemanager/v3#OperationsService.Get
	}
	return nil
}
