# Technical Deepdive

## Resource Resolution and Verification

As a result of a successful reconciliation, the *component controller* produces
an *artifact*. The *artifact* is a list of all *component descriptors* contained
in the transitive closure (in other words, the whole tree of component
references) of the *latest relevant version* of the *component* currently being
reconciled.

The reasoning behind this is that the *component controller* has to download
and calculate the digest of each *component descriptor* anyway, in order to
verify all of them against a particular signature. As this download can be a
quite expensive process (depending on the size of the *component version tree*)
it is worth trying to prevent repeating it. To be consistent in this behaviour
(for reasons that become apparent in the following section), the *component
controller* even produces this artifact if no verification is done.

Within the *Resource* custom resource, the *ocm resource* to be downloaded and
exposed as *artifact* by the *resource controller* is specified by a
***relative** resource reference*, thus, by a path of *component references*
with the *component version* currently being reconciled as the *root*. To
resolve this path, the *resource controller* would have to download all the
*component versions* that are part of this path of *component references*.

In case the component specified signatures to be verified, the *resource
controller* will also attempt to verify the *resource*. Therefore, the
downloaded content has to be digested and compared with the digest in the
*component descriptor*. To ensure that the digest within the *component
descriptor* has not been tempered with, the *resource controller* would
**again** have to verify the signature. As already explained above for the
*component controller*, to do this, the *resource controller* would also have to
download the *component descriptors* of the whole subtree of *component
versions* below the *component version* containing the resource.

Instead, the *resource controller* uses the list of *component descriptors*
exposed as *artifact* by the *component controller* to resolve the *relative
resource reference*. In case the component specified signatures to be verified,
the *component descriptors* exposed as *artifact* are already verified, and can
therefore be used to compare and verify the digest of the resource.

