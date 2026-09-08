# Replication

* Status: proposed
* Deciders: @fabianburth @jakobmoellerdev @frewilhelm @ikhandamirov

Technical Story:

The replication controller integrated into the ocm-k8s-toolkit mimics the `ocm transfer` behaviour into a controller. This allows transferring OCM components from one OCI registry to another one, e.g. as part of a delivery pipeline or a backup procedure. A possible use case would be that the replication controller is running in a Management / Control-Plane cluster.

One can use a Component custom resource to subscribe to a OCM component stored in an OCI registry. Each time the Component resource is reconciled, the current version of the OCM component is written to the resource's status. The Component resource is referenced by one or several Replication custom resources, thus the replication controller is able to detect changes in the Component's status. So, if a version change is detected, the replication controller will trigger an OCM transfer operation of the latest component version to the configured target environment, specified by an Repository custom resource.

# Problem Statement and Proposed Solutions

## Problem 1: Should a Replication be done for the latest component version in Component's Status or for the SemVer in Component's Spec?

Options:

* Option 1: Replicate all component versions fitting to semver specified in the Component resource's Spec.
* Option 2: Only replicate one single latest version mentioned in the Component resource's Status.

Proposed solution:

* Option 2, i.e. only replicate the version mentioned in the status of the Component resource. In this case we can be sure that we replicate a state of the Component that has been successfully reconciled and fulfills certain prerequisites. And this approach matches the behavior of OCM CLI.

Possible consequences:

* In case there is customer demand, a possibility to replicate all versions or a range of versions would have to be added later. Btw., OCM library currently does not support replication of a version range. Same functionality would have to be added to OCM CLI, to have feature parity with the replication controller.
* It can happen that some in-between versions have not been replicated for some reason, and the Component resource is already on a greater version. In this case a manual workaround would be possible: either creation of a dedicated set of resources (Replication + Component) covering the missing version or a transfer with OCM CLI.
* It should be explicetly decided, if it is acceptable to wait for customer demand, see [respective issue](https://github.com/open-component-model/ocm-project/issues/357).

## Problem 2: Should a successful replication automatically result in creation of k8s resources in the target environment?

The target OCI registry might also be watched by a set of ocm-k8s-toolkit controllers. Then the question could araise, if after a successful transfer of an OCM component to the target environment a corresponding set of k8s resources (i.e. Component, Repository etc.) has to be created or updated there, e.g. if not existing yet.

Options:

* Option 1: Yes, always create.
* Option 2. Yes, if possible and desirable by the user.
* Option 3. No.

Proposed solution:

* Option 3, i.e. the replication process will not create custom resources in the target environment. This goes beyond the functionality of OCM transfer. Also the assumption is that in vast majority of use cases the cluster controlling the target environment is not the one handling the current replication. And creation of resources on a different cluster is out of the scope for the replication controller.

## Problem 3: How many "historical records" does a single Replication resource need to keep in the Status?

A "historical record" contains certain information about a single transfer operation, like for example:

* Name and version of the transferred OCM component.
* Target repository URL.
* Information about success or failure of the transfer operation, etc.

So, if a Replication resource is continuosly reconciled, information about how many previous invocations do we want to keep in the status?

Options:

* Option 1: Unlimited.
* Option 2: Hardcoded limit.
* Option 3: Resource-specific limit + default (for cases where the number is omitted in the resource).
* Option 4: Retain information about invocations that are not older then X days.

Proposed solution:

* Option 3, as the most flexible. The default for the number of records in the history is 10. It can be overriden in
  each specific CR.

Possible consequences:

* Older records will be automatically removed from history (Component's status) and cannot be viewed/restored afterwards.

## Problem 4: Should the Replication controller make use of component descriptors cached by the Component controller?

As a matter of fact, the Component controller caches the component descriptors of the OCM components it is working on. At the moment of writing such a cached component descriptor is represented as a dedicated Artifact custom resource, which is referenced from the status of the Component resource. In the future the implementation will likely change. But the question remains the same - should the replication controller try to re-use the cached component descriptor?

Options:

* Option 1: No, provide the OCM Library with OCM coordinates of the source component and let it download the component version from the source repository from scratch.
* Option 2: Use a custom source provider that allows the OCM Library to consume the Artifact (or its future replacement) and fetch the required data from the local HTTP URL. For this we might need a custom repository mapping that would expose it from a CTF.

Proposed solution:

* Option 1, because it is much simplier to implement. The other option would require a complex logic, while the benefits
  are unclear.

## Problem 5: When should a new replication process be triggered?

A Replication resource is being continuously reconciled. So the question is, under which circumstances does the controller need to trigger a new OCM transfer operation?

Proposed solution is to trigger the actual replication, if one of the below conditions applies:

* If the combination of component name, component version, target repository URL and transfer options is not contained in the replication history (status of the Replication resource) yet.
* If the provided component version cannot be looked up in the target repository, even if the same transfer operation is already recorded in the history.

Possible consequences:

* Can it happen that the component version changes its content behind the scenes? If so, how would we check that,
  probably the component version digest should be used here as transfer confirmation instead of name+version? This is to
  be evaluated as part of <https://github.com/open-component-model/ocm-project/issues/341>.

## Problem 6: How to specify the transfer options?

Options:

* Option 1: As dedicated fields of the Replication resource, one field per transfer option.
* Option 2: As ocmconfig of config type 'transport.ocm.config.ocm.software' stored in a separate resource, e.g. ConfigMap.

Proposed solution:

* Option 2, because ocmconfig is the canonical way to provide configuration to OCM Library and OCM CLI.  

Possible consequences:

* Config type 'transport.ocm.config.ocm.software' would need to be extended to support more command line options (e.g.
  `--disable-uploads`). This extension looks logical, as the current differences between the config type and the command
  line options seem like a mismatch. See also <https://github.com/open-component-model/ocm-project/issues/342>.
* The same logic also applies to other types of configuration, which will then be provided to the Replication controller as custom resources containing ocmconfig:
  * Resolver configuration (config type 'ocm.config.ocm.software', similar to `--lookup` CLI command).
  * Uploader configuration (config type 'uploader.ocm.config.ocm.software', similar to `--uploader` CLI command).
  * Credentials configuration, of course (config type 'credentials.config.ocm.software').
  * And potentially all the other [supported configuration types](https://github.com/open-component-model/ocm/blob/main/docs/reference/ocm_configfile.md).

## Problem 7: Do we need to store ocmconfig with transfer options in a dedicated field in the Replication's Spec?

Options:

* Option 1: No, use generic ConfigRefs
* Option 2: Yes, use a dedicated field with a reference to a ConfigMap

Decision driver: an SRE might need a way to quickly find which transfer options an individual replication run was executed with.

Note: same question may apply to any kind of configuration.

Proposed solution:

* Use option 1 (generic ConfigRefs), because this is a common pattern across ocm-k8s-toolkit controllers.

Possible consequences:

* It has to be evaluated, whether we want to store a certain specific configuration, which a transfer operation was
  executed with, and which does not contain sensitive infomation, like e.g. the transfer options, in a human readable
  form in the status as part of the Replication's history. The latter would simplify support and problem analysis by an
  SRE. For transfer options this is addressed in <https://github.com/open-component-model/ocm-project/issues/355>.

## Problem 8: Should a Replication resource support several target Repository resources?

Options:

* Option 1: No, one Replication resource referrs to exactly one target Repository resource.
* Option 2: Yes, one Replication resource should be able to control replication to several target repositories, e.g. if there is a need to distribute a Component across several locations.

Proposed solution:

* Option 1, because it is easier to understand and to keep track of. This also corresponds to how the OCM CLI is currently working. It is easy to set up several sets of resources (Replication + Repository) to enable distribution to several locations. On the other hand, if there will be customer demand, we can always create a "ReplicationSet" later on that can deal with multiple targets.
