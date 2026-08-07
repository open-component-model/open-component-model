---
title: "Display and Examine Component Versions"
description: "Learn how to display and examine component versions in OCM."
icon: "👁️"
weight: 24
toc: true
---

## List Component Versions

To show a component stored in an OCM repository or CTF archive (which itself is an OCM repository), the [`ocm get componentversion`](https://github.com/open-component-model/ocm/blob/main/docs/reference/ocm_get_componentversions.md) command can be used:

```shell
ocm get componentversion ghcr.io/open-component-model/ocm//ocm.software/toi/demo/helmdemo:0.12.0
```

```shell
  COMPONENT                      VERSION PROVIDER
  ocm.software/toi/demo/helmdemo 0.12.0  ocm.software
```

To see the component descriptor of the displayed component version, use the output format option `-o yaml`:

```shell
ocm get cv ghcr.io/open-component-model/ocm//ocm.software/toi/demo/helmdemo:0.12.0 -o yaml
```

```yaml
component:
  componentReferences:
  - componentName: ocm.software/toi/installers/helminstaller
    name: installer
    version: 0.12.0
  creationTime: "2024-07-19T14:32:13Z"
  name: ocm.software/toi/demo/helmdemo
  provider: ocm.software
  repositoryContexts:
  - baseUrl: ghcr.io
    componentNameMapping: urlPath
    subPath: open-component-model/ocm
    type: OCIRegistry
  resources:
  - access:
      localReference: sha256:8a2fe6af4ce56249094622c9d618e24b4cfb461a7dfa6a42cce31749189bc499
      mediaType: application/vnd.toi.ocm.software.package.v1+yaml
      type: localBlob
    digest:
      ...
    labels:
    - name: commit
      value: e5ca3001323b75ee5793a786089f1f410e9e8db3
    name: package
    relation: local
    type: toiPackage
    version: 0.12.0
  - access:
      imageReference: ghcr.io/open-component-model/ocm/ocm.software/toi/demo/helmdemo/echoserver:0.1.0
      type: ociArtifact
    digest:
      ...
    name: chart
    relation: local
    type: helmChart
    version: 0.12.0
...
```

To refer to the content of a component repository, the component name can be appended to the repository specification separated by `//` (you can also use the `--repo` option to specify the repository).

In the example above, `ghcr.io/open-component-model/ocm` is the OCM repository, whereas `ocm.software/toi/demo/helmdemo` is the component stored in this component repository.

Optionally, a specific version can be appended, separated by a colon (`:`). If no version is specified, all component versions will be displayed.

With the option `--recursive`, it is possible to show the complete component version, including the component versions it references.

```shell
ocm get cv ghcr.io/open-component-model/ocm//ocm.software/toi/demo/helmdemo:0.12.0 --recursive
```

```shell
  REFERENCEPATH                         COMPONENT                                 VERSION PROVIDER     IDENTITY
                                        ocm.software/toi/demo/helmdemo            0.12.0  ocm.software
  ocm.software/toi/demo/helmdemo:0.12.0 ocm.software/toi/installers/helminstaller 0.12.0  ocm.software "name"="installer"
```

To get a tree view, add the option `-o tree`:

```shell
ocm get cv ghcr.io/open-component-model/ocm//ocm.software/toi/demo/helmdemo:0.12.0 --recursive -o tree
```

```shell
  NESTING COMPONENT                                 VERSION PROVIDER     IDENTITY
  └─ ⊗    ocm.software/toi/demo/helmdemo            0.12.0  ocm.software
     └─   ocm.software/toi/installers/helminstaller 0.12.0  ocm.software "name"="installer"
```

As mentioned before a CTF archive itself is an OCM repository, so we can execute the same commands on a CTF archive. So, let's get the information about the component `github.com/acme.org/helloworld` we created in the previous step and that we stored in the CTF archive `/tmp/helloworld/ctf-hello-world`:

```shell
ocm get cv /tmp/helloworld/ctf-hello-world//github.com/acme.org/helloworld:1.0.0
```

```shell
  COMPONENT                       VERSION  PROVIDER
  github.com/acme.org/helloworld  0.1.0    ocm.software
```

## List the Resources of a Component Version

To list the resources found in a component version tree, the command [`ocm get resources`](https://github.com/open-component-model/ocm/blob/main/docs/reference/ocm_get_resources.md) can be used:

```shell
ocm get resources ghcr.io/open-component-model/ocm//ocm.software/toi/demo/helmdemo:0.12.0 --recursive -o tree
```

```shell
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

## Download the Resources of a Component Version

Use the [`ocm download`](https://github.com/open-component-model/ocm/blob/main/docs/reference/ocm_download_resources.md) command to download resources such as component versions, individual resources or artifacts:

```shell
ocm download resource ghcr.io/open-component-model/ocm//ocm.software/toi/demo/helmdemo:0.12.0 chart -O helmchart.tgz
```

```shell
  helmchart.tgz: 4707 byte(s) written
```

Because it is stored as OCI artifact in an OCI registry, the filesystem format used for OCI artifacts is the blob format.

<details><summary>What happened?</summary>

The file `helmchart.tgz` was downloaded.

```shell
tar xvf helmchart.tgz
```

```shell
  x index.json
  x oci-layout
  x blobs
  x blobs/sha256.a9dd654eed17e786b5c5445e8bc48f3a47371c2efe392a53a3fbecd9e942b696
  x blobs/sha256.c8017985866ceb44c2426a4ad9a429d6aec1f6818cb6dccbf964623139c1d1d5
  x blobs/sha256.ea8e5b44cd1aff1f3d9377d169ad795be20fbfcd58475a62341ed8fb74d4788c
```

```shell
jq . index.json
```

```json
{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.index.v1+json",
  "manifests": [
    {
      "mediaType": "application/vnd.oci.image.manifest.v1+json",
      "digest": "sha256:c8017985866ceb44c2426a4ad9a429d6aec1f6818cb6dccbf964623139c1d1d5",
      "size": 410,
      "annotations": {
        "org.opencontainers.image.ref.name": "0.1.0",
        "software.ocm/tags": "0.1.0"
      }
    }
  ],
  "annotations": {
    "software.ocm/main": "sha256:c8017985866ceb44c2426a4ad9a429d6aec1f6818cb6dccbf964623139c1d1d5"
  }
}
```

</details>

### Download with Download Handlers

To use a format more suitable for the content technology, enable the usage
of download handlers.

If a download handler is available for the artifact type and the
blob media type used to store the blob in the OCM repository, it will convert the blob format
into a more suitable format:

```shell
ocm download resource -d ghcr.io/open-component-model/ocm//ocm.software/toi/demo/helmdemo:0.12.0 chart -O helmchart.tgz
```

```shell
  helmchart.tgz: 3763 byte(s) written
```

<details><summary>What happened?</summary>

The downloaded archive is now a regular Helm Chart archive:

```shell
tar tvf helmchart.tgz
```

```shell
  -rw-r--r--  0 0      0         136 Jul 19 16:32 echoserver/Chart.yaml
  -rw-r--r--  0 0      0        1842 Jul 19 16:32 echoserver/values.yaml
  -rw-r--r--  0 0      0        1755 Jul 19 16:32 echoserver/templates/NOTES.txt
  -rw-r--r--  0 0      0        1802 Jul 19 16:32 echoserver/templates/_helpers.tpl
  -rw-r--r--  0 0      0        1848 Jul 19 16:32 echoserver/templates/deployment.yaml
  -rw-r--r--  0 0      0         922 Jul 19 16:32 echoserver/templates/hpa.yaml
  -rw-r--r--  0 0      0        2083 Jul 19 16:32 echoserver/templates/ingress.yaml
  -rw-r--r--  0 0      0         367 Jul 19 16:32 echoserver/templates/service.yaml
  -rw-r--r--  0 0      0         324 Jul 19 16:32 echoserver/templates/serviceaccount.yaml
  -rw-r--r--  0 0      0         385 Jul 19 16:32 echoserver/templates/tests/test-connection.yaml
  -rw-r--r--  0 0      0         349 Jul 19 16:32 echoserver/.helmignore
```

</details>

### Download an Image

For example, for OCI images, the OCI format is more suitable:

```shell
ocm download resource ghcr.io/open-component-model/ocm//ocm.software/toi/demo/helmdemo:0.12.0 image -O image.tgz
```

```shell
  image.tgz: 46181313 byte(s) written
```

<details><summary>What happened?</summary>

The file `image.tgz` was downloaded.

```shell
tar xvf image.tgz
```

```shell
  x index.json
  x oci-layout
  x blobs
  x blobs/sha256.06679f57dba70a6875e4ae5843ba2483ecab6ec48182ca8720ddc5b1863bad52
  x blobs/sha256.28c6282d04f63710146ace6c7be14a40c7ee6a71a2f91316928469e4aafe0d92
  x blobs/sha256.2d3e25b9e93ad26878862abee5ed02683206f6f6d57e311cdd1dedf3662b61c8
  x blobs/sha256.365ec60129c5426b4cf160257c06f6ad062c709e0576c8b3d9a5dcc488f5252d
  x blobs/sha256.4b12f3ef8e65aaf1fd77201670deb98728a8925236d8f1f0473afa5abe9de119
  x blobs/sha256.76d46396145f805d716dcd1607832e6a1257aa17c0c2646a2a4916e47059dd54
  x blobs/sha256.7fd34bf149707ca78b3bb90e4ba68fe9a013465e5d03179fb8d3a3b1cac8be27
  x blobs/sha256.b0e3c31807a2330c86f07d45a6d80923d947a8a66745a2fd68eb3994be879db6
  x blobs/sha256.bc391bffe5907b0eaa04e96fd638784f77d39f1feb7fbe438a1dae0af2675205
  x blobs/sha256.cb5c1bddd1b5665e1867a7fa1b5fa843a47ee433bbb75d4293888b71def53229
  x blobs/sha256.d5157969118932d522396fe278eb722551751c7aa7473e6d3f03e821a74ee8ec
  x blobs/sha256.e0962580d8254d0b1ef35006d7e2319eb4870e63dc1f9573d2406c7c47d442d2
```

```shell
jq . index.json
```

```json
{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.index.v1+json",
  "manifests": [
    {
      "mediaType": "application/vnd.docker.distribution.manifest.v2+json",
      "digest": "sha256:cb5c1bddd1b5665e1867a7fa1b5fa843a47ee433bbb75d4293888b71def53229",
      "size": 2400,
      "annotations": {
        "org.opencontainers.image.ref.name": "1.10",
        "software.ocm/tags": "1.10"
      }
    }
  ],
  "annotations": {
    "software.ocm/main": "sha256:cb5c1bddd1b5665e1867a7fa1b5fa843a47ee433bbb75d4293888b71def53229"
  }
}
```

</details>

### Download an Executable

The Open Component Model allows to publish platform-specific executables. In this case, the platform
specification is used by convention as extra identity for the artifacts that are contained in the component
version.

Example:

```shell
ocm get componentversion ghcr.io/open-component-model/ocm//ocm.software/ocmcli:0.1.0-dev -o yaml
```

```yaml
...
    resources:
    - name: ocmcli
      extraIdentity:
        architecture: amd64
        os: linux
      relation: local
      type: executable
      version: 0.1.0-dev
      access:
        localReference: sha256:1a8827761f0aaa897d1d4330c845121c157e905d1ff300ba5488f8c423bc7cd9
        mediaType: application/octet-stream
        type: localBlob
    - name: ocmcli
      extraIdentity:
        architecture: arm64
        os: darwin
      relation: local
      type: executable
      version: 0.1.0-dev
      access:
        localReference: sha256:9976b18dc16ae2b2b3fc56686f18f4896d44859f1ea6221f70e83517f697e289
        mediaType: application/octet-stream
        type: localBlob
...
```

Note that the resources shown above have the same name and type `executable` but a different extra-identity. If a
component version complies to this convention, executables can directly be downloaded for the specified
platform with the use of the `-x` option. If only one executable is contained in the component version, the
resource name can be omitted. Example:

```shell
ocm download resource -x --latest ghcr.io/open-component-model/ocm//ocm.software/ocmcli
```

```shell
  ocm: 83369938 byte(s) written
```

<details><summary>What happened?</summary>

```shell
file ocm
```

```shell Mach-O 64-bit executable arm64
```

With the option `--latest`, the latest matching component version is used for download. With the
option `--constraints`, version constraints can be configured. For example: `--constraints 0.1.x`
will select all patch versions of `0.1`. Together with `--latest`, the latest patch version is
selected.

The option `-x` enables the executable download handler, which provides the x-bit of the downloaded
files. Additionally, it filters all matching resources for executables and the correct platform.

</details>

### Download a Full Component Version

Download entire component versions using the [`ocm download componentversion`](https://github.com/open-component-model/ocm/blob/main/docs/reference/ocm_download_componentversions.md) command:

```shell
ocm download componentversions ghcr.io/open-component-model/ocm//ocm.software/toi/demo/helmdemo:0.12.0 -O helloworld
```

```shell
  helloworld: downloaded
```

The result is a CTF archive. This can then be modified using the `ocm add ...` commands shown earlier.

<details><summary>What happened?</summary>

The component version was downloaded.

```shell
tree helloworld
```

```shell
  helloworld/
  ├── blobs
  │   ├── sha256.87cef1e2233bf5591030ac854e2556fbe6a00a28bb5640e25a9cb69ece519c5a
  │   ├── sha256.8a2fe6af4ce56249094622c9d618e24b4cfb461a7dfa6a42cce31749189bc499
  │   └── sha256.e790920a11de2016de64225280efcf062e14b767955f7508de64fd5192e3fb3a
  └── component-descriptor.yaml
```

</details>

## Download OCI Artifacts

Download OCI artifacts from an OCI registry, such as OCI images, with the [`ocm download artifacts`](https://github.com/open-component-model/ocm/blob/main/docs/reference/ocm_download_artifacts.md) command:

```shell
ocm download artifact ghcr.io/open-component-model/ocm-controller:v0.24.0 -O ocm-controller
```

```shell
  ocm-controller: downloaded
```

<details><summary>What happened?</summary>

The OCI image `echoserver` was downloaded.

```shell
tree echoserver
```

```shell
  ocm-controller/
  ├── blobs
  │   ├── sha256.05d57e68048827c243cd477025f96064df9f4d83b8639ed04306f0647c9cfe78
  │   ├── sha256.0f8b424aa0b96c1c388a5fd4d90735604459256336853082afb61733438872b5
  │   ├── sha256.1069fc2daed1aceff7232f4b8ab21200dd3d8b04f61be9da86977a34a105dfdc
  │   ├── sha256.286c61c9a31ace5fa0b8832c8e8e30d66bf32138f2f787463235aa0071f714ea
  │   ├── sha256.2bdf44d7aa71bf3a0da2de0563ad0e3882948d699b4991edf8c0ab44e7f26ae3
  │   ├── sha256.35fddc32f468fc8d276fa1b6a72cac27f35a0080233c2ddc6a03fab224024dbc
  │   ├── sha256.3f4e2c5863480125882d92060440a5250766bce764fee10acdbac18c872e4dc7
  │   ├── sha256.452e9eed7ecfd0c2b44ac6fda20cee66ab98aec38ba30aa868e02445be7c8bb0
  │   ├── sha256.80a8c047508ae5cd6a591060fc43422cb8e3aea1bd908d913e8f0146e2297fea
  │   ├── sha256.9375d0c4fac611287075434624a464af5b6bb026947698a06577ad348f607d56
  │   ├── sha256.b40161cd83fc5d470d6abe50e87aa288481b6b89137012881d74187cfbf9f502
  │   ├── sha256.c8022d07192eddbb2a548ba83be5e412f7ba863bbba158d133c9653bb8a47768
  │   ├── sha256.d557676654e572af3e3173c90e7874644207fda32cd87e9d3d66b5d7b98a7b21
  │   └── sha256.d858cbc252ade14879807ff8dbc3043a26bbdb92087da98cda831ee040b172b3
  ├── index.json
  └── oci-layout
```

</details>
