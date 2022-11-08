exit # not that kind of script. Run commands below manually.

DEVSHELL_PROJECT_ID=my-project-name # Usually set automatically in Cloud Shell
APP_NAME=manage-secure-tags
REGION=us-central1 # Must be us-central1 per https://cloud.google.com/eventarc/docs/issues
ORGID=10123456789  # From gcloud organizations list

# Enable required services
gcloud services enable --project=$DEVSHELL_PROJECT_ID \
    cloudresourcemanager.googleapis.com \
    artifactregistry.googleapis.com \
    cloudbuild.googleapis.com \
    run.googleapis.com \
    eventarc.googleapis.com   

# Create service account with correct IAM permissions to manage tags
RUNSERVICEACCOUNT="$APP_NAME@$DEVSHELL_PROJECT_ID.iam.gserviceaccount.com"
gcloud iam service-accounts create $APP_NAME --project $DEVSHELL_PROJECT_ID
# Permissions for binding secure tags on all resources in org. See https://cloud.google.com/resource-manager/docs/tags/tags-creating-and-managing
gcloud organizations add-iam-policy-binding $ORGID \
 --member="serviceAccount:$RUNSERVICEACCOUNT" \
 --role="roles/resourcemanager.tagUser"

# Create service account with correct IAM permissions to access cloud run/pubsub
TRIGGERSERVICEACCOUNT="$APP_NAME-invoker@$DEVSHELL_PROJECT_ID.iam.gserviceaccount.com"
gcloud iam service-accounts create "$APP_NAME-invoker" --project $DEVSHELL_PROJECT_ID
# https://cloud.google.com/eventarc/docs/roles-permissions
gcloud run services add-iam-policy-binding $APP_NAME --region $REGION --project $DEVSHELL_PROJECT_ID \
  --member="serviceAccount:$TRIGGERSERVICEACCOUNT" \
  --role='roles/run.invoker'


# Deploy manage-secure-tags service https://cloud.google.com/sdk/gcloud/reference/run/deploy
gcloud run deploy $APP_NAME --source ./manage-secure-tags --region $REGION --project=$DEVSHELL_PROJECT_ID \
  --service-account $RUNSERVICEACCOUNT --ingress=internal --no-allow-unauthenticated

# Setup pubsub topic for cross project log-sink
TOPIC="v1-compute-instances"
gcloud pubsub topics create $TOPIC --project $DEVSHELL_PROJECT_ID


# Create org wide aggregated sink https://cloud.google.com/logging/docs/export/aggregated_sinks#gcloud
gcloud logging sinks create $APP_NAME --organization=$ORGID --include-children \
  --log-filter='resource.type=gce_instance AND protoPayload.methodName="v1.compute.instances.insert"' \
  "pubsub.googleapis.com/projects/$DEVSHELL_PROJECT_ID/topics/$TOPIC"

# Or add individual sinks in each compute project https://cloud.google.com/eventarc/docs/cross-project-triggers#audit-logs-events
gcloud logging sinks create $APP_NAME --project example-compute-project \
  "pubsub.googleapis.com/projects/$DEVSHELL_PROJECT_ID/topics/$TOPIC" \
  --log-filter='protoPayload.methodName="v1.compute.instances.insert"'

# Add the service account displayed when creating the sink(s) as publisher for pubsub topic
gcloud pubsub topics add-iam-policy-binding $TOPIC \
    --member="serviceAccount:<listed-value-here>@gcp-sa-logging.iam.gserviceaccount.com" \
    --role=roles/pubsub.publisher


# direct trigger (when not using aggregated/cross-project sink) https://cloud.google.com/sdk/gcloud/reference/eventarc/triggers/create
# Note: this trigger only works for VMs in same project as cloud run (no log sink required). See below for aggregated/cross project trigger
gcloud eventarc triggers create new-gce-vm-direct --project=$DEVSHELL_PROJECT_ID \
  --destination-run-service=$APP_NAME \
  --destination-run-region=$REGION \
  --destination-run-path="/v1/compute/instances" \
  --event-filters="type=google.cloud.audit.log.v1.written" \
  --event-filters="serviceName=compute.googleapis.com" \
  --event-filters="methodName=v1.compute.instances.insert" \
  --service-account=$PROJECT_NUMBER-compute@developer.gserviceaccount.com

# Trigger for aggregated or cross-project events that are added to pubsub topic
gcloud eventarc triggers create new-gce-vm-pubsub --project=$DEVSHELL_PROJECT_ID \
    --location=$REGION \
    --destination-run-service=$APP_NAME \
    --destination-run-region=$REGION \
    --destination-run-path="/v1/compute/instances_pubsub" \
    --event-filters="type=google.cloud.pubsub.topic.v1.messagePublished" \
    --transport-topic="projects/$DEVSHELL_PROJECT_ID/topics/$TOPIC" \
    --service-account=$TRIGGERSERVICEACCOUNT

# When running manage-secure-tags service locally you can test using
curl -X POST -H "Content-Type: application/json" -d "@../sample-gce-vm-insert-first.json" localhost:8080/v1/compute/instances
