---
title: "We did a Hackathon!"
description: "We recently partook in an adventure @ SAP Innovation Center in Potsdam where we got together and worked on several valuable project for the IPCEI-CIS initiative. Here is what we did."
date: 2026-08-03T10:00:00+02:00
contributors: []
tags: ["ocm", "thalamus", "sbom", "neonephos"]
draft: false
---

## Hackathon @ SAP Innovation Center Potsdam

From 21.07.2026 to 24.07.2026 we attended an SAP Hackathon @ [SAP Innovation Center in Potsdam](https://maps.app.goo.gl/xWjXeojfLuXJdyzw8). This is
one of, if not the best, locations that SAP has around the globe. SAP was so nice to provide us with food, drinks and a
lovely set of people to work together with on interesting projects in the IPCEI-CIS initiative.

The [IPCEI-CIS Initiative](https://www.8ra.com/) is a movement that brings sovereignty to Cloud Infrastructure and Services.
To that end, parts of the OCM team arrived in Potsdam on Monday and started furiously hacking on
projects from Tuesday to Thursday to then present them to a larger audience at the end of Thursday. The audience included people from all 
around the globe joining in on an effort to bring independence to the people using Cloud services in Europe. We had some
great fun, and it was quite refreshing to enjoy a bit of time off from meetings to just _hack_ on the projects that we feel
passionate about.

Here you can meet the team.
![The hackathon team at SAP Innovation Center Potsdam](/images/ipcei-cis-potsdam-the-team.png)

We worked on several projects, but most notably, we worked on these three:

### Shipping SBOMs with your component

This is a proof of concept for transporting SBOMs together with your Component Version in order to make them more discoverable,
linkable and help orchestrating SBOMs so the whole component could be scanned in one command. This was tied together with
[OpenDeliveryGear (ODG)](https://open-component-model.github.io/open-delivery-gear/) and using VEX (Vulnerability Exploitability eXchange) statements inside the SBOM
to showcase a pretty cool frontend.

You can read more about this in our detailed blog post at [Shipping SBOMs with your component](https://ocm.software/blog/2026-07-28-shipping-sboms-with-your-components).

### Thalamus project OCM-ification

OCM-ifying your application can be a daunting challenge, especially if you have many moving parts and even more components
that you need to transfer and then verify and then localize. Localization in the OCM world means that we update image references
that point to location A to the transfer destination which points to location B. For example transferring from GHCR to
a private registry like Zot, you want all the images in your values.yaml to be fetched from Zot, including any third-party
image sources (e.g. an image for PostgreSQL). 

This is achieved using Kro + Fairy Dust from OCM.

You can read more about the fairy dust part in [Creating an OCM component for a non-trivial application](https://ocm.software/blog/2026-07-30-ocmifying-thalamus) blog post.

### Component Discovery API

The component versions that we provide and use in Kubernetes clusters have information about dependencies,
artifacts, locations, deployments, ownership, and other metadata.

This information from within the cluster, however, cannot be easily accessed since it's not displayed anywhere. If a user wants
to know where something is located, they need to use OCM as a library or the CLI to query this information from a
Component Version in the cluster.

This is where a discovery API would be useful. Since you are already in the cluster, why not just create an object
that uses our [ocm-k8s-toolkit](https://github.com/open-component-model/open-component-model/tree/main/kubernetes/controller) to figure this out for you?

The new object `Discovery` provides this API. Its design is described in the [Designing a Discovery API](https://ocm.software/blog/designing-a-discovery-api/) blog post.

## Conclusion

We had a blast bringing you these projects that we hope further help the IPCEI-CIS effort of providing independent cloud infrastructure
for European citizens, and hope to see you soon again in Potsdam, or at another lovely location.

And we weren't the only ones who had fun!

You can read about the Thalamus project's experience over at their blog post [IPCEI-CIS Hackathon @ SAP Innovation Center Potsdam](https://cobaltcore-dev.github.io/thalamus/main/ipcei-cis-workshop-2026/).
