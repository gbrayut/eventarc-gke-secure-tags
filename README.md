# Eventarc for managing Secure Tags on GKE or MIG VMs

## Overview

[Eventarc](https://cloud.google.com/eventarc/docs/overview) lets you asynchronously deliver
events from different event sources (Google Cloud sources with Audit Logs, Cloud
Storage buckets, and Pub/Sub topics) to different event consumers (Cloud Run
services, Cloud Functions, Workflows and GKE services).

This repository contains an example for adding [Secure Tags](https://github.com/gbrayut/cloud-examples/tree/main/gce-firewall-rules#resource-manager-tags-secure-iam-tags) to GKE nodes. This is intended as a Proof of Concept workaround until Secure Tags are natively supported by Managed Instance Groups and GKE Node Pools.

You can see the basic deployment process in [setup.sh](./setup.sh), and for [cross-project Eventarc support](https://cloud.google.com/eventarc/docs/cross-project-triggers#audit-logs-events) it uses a Log Sink and Pub/Sub topic.

![cross project audit logs](https://cloud.google.com/static/eventarc/docs/images/cross-project-audit-logs.svg)

For matching VMs to tags this project looks for 'stv-123456789' style Network Tags on each node. That lookup could easily be replaced with a different data source like Secret Manager or a database.

## Testing Latency of creating Tag Bindings

Since VMs/MIGs/NodePools do not yet support adding tags when VMs are created, there initially were some concerns over the delay for bindings to take effect on new VMs. Some testing shows that by using the v1.compute.instances.insert AuditLog events, the bindings should usually be created within 10-20 seconds of when the new VM API call was made. This means unless there is an issue delivering the audit logs, the tags should be applied before the network-online systemd target is reached. And for the case of GKE, which spends 2-6 minutes running cloud-init scripts for each node, the tags should be in effect well before the kubelet has started or any workloads are scheduled.

```
Timing tests:

creationTimestamp: '2022-11-01T20:26:40.350-07:00'
pubsub              2022-11-02T03:26:45.196Z
tagop started       2022/11/02 03:26:47 operations/rctb.us-west1-a.6711501896331814768
auditlogtimestamp: "2022-11-02T03:26:51.979269Z"

lastStartTimestamp:     2022-11-01T20:26:46.342-07:00'
network.target          2022-11-02 03:27:02 UTC
network-online.target   2022-11-02 03:27:07 UTC
multi-user.target       2022-11-02 03:27:28 UTC

Search logs at https://console.cloud.google.com/logs/query for VM/GKE project with query:
resource.type="audited_resource"
resource.labels.method="google.cloud.resourcemanager.v3.TagBindings.CreateTagBinding"
resource.labels.service="cloudresourcemanager.googleapis.com"

GKE:
lastStartTimestamp      2022-11-01T21:10:20.222-07:00'
kubelet.service         2022-11-02 04:10:53 UTC
CreateTagBinding        2022-11-02T04:10:25.139369Z"

node CreationTimestamp:  Tue, 01 Nov 2022 22:14:28 -0600
node kubeletReady              2022-11-02T04:15:09Z
```

That said, there is always the possibility that AuditLogs could be delayed or even missing. If desired you could also use [node-problem-detector](https://kubernetes.io/docs/tasks/debug/debug-cluster/monitor-node-health/) or a custom Daemonset to ensure the expected tags are in place on each node.

## Additional Resources

* https://cloud.google.com/eventarc/docs/run/cal
* More [Eventarc Samples](https://github.com/GoogleCloudPlatform/eventarc-samples)
* https://cloud.google.com/eventarc/docs/run/pubsub-authenticated
* https://cloud.google.com/eventarc/docs/run/debugging-events-cloud-run

## Future Features?

* Better error handling (error-topic, structured logs, googleapi ratelimit/backoff)
* Other endpoints to help apply new tags to existing VMs/MIGs
* Replace genproto with custom type (from https://mholt.github.io/json-to-go/)

-------

This is not an official Google product. The software is provided "As is" without warranty or support of any kind.
