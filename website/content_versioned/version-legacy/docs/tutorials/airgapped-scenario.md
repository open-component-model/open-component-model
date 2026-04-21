---
title: "Transport to Air-Gapped Environments"
description: "Learn how to transfer OCM components to air-gapped registries."
icon: "🚚"
weight: 48
toc: true
---

Working with air-gapped environments requires careful preparation and a systematic approach to ensure all component dependencies are available offline. This section demonstrates how to prepare, transport, and deploy OCM components in secure, disconnected environments.

### Inspect Component Structure

Begin by examining the component descriptor and its resolved references to understand the complete dependency tree:

```shell
ocm get resources ghcr.io/open-component-model/ocm//ocm.software/toi/demo/helmdemo:0.12.0 -r  -otree
COMPONENT                                       NAME           VERSION IDENTITY TYPE        RELATION
└─ ocm.software/toi/demo/helmdemo                              0.12.0                       
   ├─                                           chart          0.12.0           helmChart   local
   ├─                                           config-example 0.12.0           yaml        local
   ├─                                           creds-example  0.12.0           yaml        local
   ├─                                           image          1.0              ociImage    external
   ├─                                           package        0.12.0           toiPackage  local
   └─ ocm.software/toi/installers/helminstaller installer      0.12.0                       
      ├─                                        toiexecutor    0.12.0           toiExecutor local
      └─                                        toiimage       0.12.0           ociImage    local
```

### Create Offline Package

Download the complete component descriptor with all referenced resources as local blobs. This creates a self-contained package that includes all dependencies:

```shell
ocm transfer component ghcr.io/open-component-model/ocm//ocm.software/toi/demo/helmdemo:0.12.0 -r --copy-resources ./ctf-copy-resources
transferring version "ocm.software/toi/demo/helmdemo:0.12.0"...
  transferring version "ocm.software/toi/installers/helminstaller:0.12.0"...
  ...resource 0 toiimage[ociImage](ocm.software/toi/installers/helminstaller/helminstaller:0.12.0)...
  ...resource 1 toiexecutor[toiExecutor]...
  ...adding component version...
...resource 0 package[toiPackage]...
...resource 1 chart[helmChart](ocm.software/toi/demo/helmdemo/echoserver:0.1.0)...
...resource 2 image[ociImage](google-containers/echoserver:1.10)...
...resource 3 config-example[yaml]...
...resource 4 creds-example[yaml]...
...adding component version...
2 versions transferred
```

### Verify Package Integrity

Confirm that all resources have been successfully packaged and are available as local blobs:

```shell
du -shA ctf-copy-resources 
106M ctf-copy-resources
```

```shell
ocm get resources ctf-copy-resources -o treewide 
COMPONENT                                    NAME           VERSION IDENTITY TYPE        RELATION ACCESS
├─ ocm.software/toi/demo/helmdemo                           0.12.0                                
│  ├─                                        chart          0.12.0           helmChart   local    localBlob
│  ├─                                        config-example 0.12.0           yaml        local    localBlob
│  ├─                                        creds-example  0.12.0           yaml        local    localBlob
│  ├─                                        image          1.0              ociImage    external localBlob
│  └─                                        package        0.12.0           toiPackage  local    localBlob
└─ ocm.software/toi/installers/helminstaller                0.12.0                                
   ├─                                        toiexecutor    0.12.0           toiExecutor local    localBlob
   └─                                        toiimage       0.12.0           ociImage    local    localBlob
```

### Transport to Air-Gapped Environment

Copy the packaged OCM artifacts to your preferred portable storage medium for secure transport:

```shell
sudo cp -r ./ctf-copy-resources /media/....
```

**Physical Transport Phase** ✈️

Transport your portable storage device containing the OCM artefact to the air-gapped environment following your organization's security protocols.

```shell
                                                   ___
                                              ____/   \____
    o  "Here I come!"                         |    ✈️     |
   /|\_┌▓┐                                    |___________|
   / \                                           |     |
                                              ___|     |___
```  

### Deploy in Air-Gapped Environment

Transfer the OCM artifacts from your portable storage device to the air-gapped OCI registry:

```shell
ocm transfer ctf ./media/.../ctf-copy-resources $AIR_GAPPED_OCI_REGISTRY/... 
```

### Deploy in Air-Gapped Environment

Transfer the OCM artifacts from your portable storage device to the air-gapped OCI registry:

```shell
ocm transfer ctf ./media/.../ctf-copy-resources $AIR_GAPPED_OCI_REGISTRY/... 
```

### Run `local` OCI image in Air-Gapped Environment

Sometimes you do not have an air-gapped OCI registry from the start and need to run a OCI image from your portable storage device.

Prerequisite: `docker` cli & `ocm` cli available.

1. Download OCM resource which you want to run as `local` image.

   ```shell
   $ ocm download resource ghcr.io/open-component-model/ocm//ocm.software/toi/demo/helmdemo:0.12.0 image -O ./local-image-1-0
   ./local-image-1-0: 46181313 byte(s) written
   ```

2. Copy the OCI image to your preferred portable storage medium for secure transport:

   ```shell
   sudo cp -r ./local-image-1-0 /media/....
   ```

3. Import OCI image to `docker`

   ```shell
   $ docker import ./local-image-1-0 helmdemo-image:1.0
   sha256:a107d637d6b8dd1d021d49b7f315f1b77eb763aec1205ad942a99e9a1255ed22

   $ docker images | grep helmdemo
   helmdemo-image 1.0 a107d637d6b8   44 seconds ago   46.2MB
   ```

4. Start Container

   ```shell
   docker run helmdemo-image:1.0 ....
    ```
