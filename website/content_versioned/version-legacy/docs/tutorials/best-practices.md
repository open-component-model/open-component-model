---
title: "Best Practices"
description: "Best practices for working with the Open Component Model."
icon: "✅"
weight: 41
toc: true
---

This chapter contains guidelines for common scenarios how to work with the Open Component Model, focusing on using CI/CD, build and publishing processes.

- [Use Public Schema for Validation and Auto-Completion of Component Descriptors](#use-public-schema-for-validation-and-auto-completion-of-component-descriptors)
- [Separate Build and Publish Processes](#separate-build-and-publish-processes)
- [Using Makefiles](#using-makefiles)
  - [Prerequisites](#prerequisites)
  - [Example Makefile](#example-makefile)
  - [Templating the Resources](#templating-the-resources)
- [Pipeline Integration](#pipeline-integration)
- [Static and Dynamic Variable Substitution](#static-and-dynamic-variable-substitution)
  - [Example Substitution File](#example-substitution-file)
- [Debugging: Explain the Blobs Directory](#debugging-explain-the-blobs-directory)
- [Self-Contained Transport Archives](#self-contained-transport-archives)
- [CICD Integration](#cicd-integration)

## Use Public Schema for Validation and Auto-Completion of Component Descriptors

The Open Component Model (OCM) provides a public schema to validate and offer auto-completion of component constructor files
used to create component descriptors.
This schema is available at [https://ocm.software/schemas/configuration-schema.yaml](https://ocm.software/schemas/configuration-schema.yaml).

To use this schema in your IDE, you can add the following line to your component constructor file:

```yaml
# yaml-language-server: $schema=https://ocm.software/latest/schemas/bindings/go/constructor/schema-2020-12.json
```

This line tells the YAML language server to use the OCM schema for validation and auto-completion.

## Separate Build and Publish Processes

Traditional automated builds often have unrestricted internet access, which can lead to several challenges in enterprise environments:

- Limited control over downloaded artifacts
- Potential unavailability of required resources
- Security risks associated with write permissions to external repositories

Best practice: Implement a two-step process:
a) Build: Create artifacts in a controlled environment, using local mirrors when possible.
b) Publish: Use a separate, secured process to distribute build results.

OCM supports this approach through filesystem-based OCM repositories, allowing you to generate Common Transport Format (CTF) archives for component versions.
These archives can then be securely processed and distributed.

## Using Makefiles

Developing applications and services using the Open Component Model usually is an iterative process
of building artifacts, generating OCM component versions and finally publishing them.
To simplify this process it should be automated and integrated into your build process.
One option is to use a `Makefile`.

The following example can be used as a starting point and can be modified according to your needs.
In this example we will use the same example as in the sections before:

- Creating a multi-arch image from Go sources from a Git repository using the Docker CLI
- Packaging the Docker image and a Helm chart into a CTF archive
- Signing and publishing the build result

### Prerequisites

- The OCM CLI must be installed and be available in your PATH
- The Makefile is located in the top-level folder of a Git project
- Operating system is Unix/Linux
- A sub-directory `local` can be used for local settings e.g. environment variables, RSA keys, ...
- A sub-directory `gen` will be used for generated artifacts from the `make build` command
- It is recommended to add `local/` and `gen/` to the `.gitignore` file

We use the following file system layout for the example:

```shell
$ tree .
.
├── Dockerfile
├── LICENSE
├── Makefile
├── README.md
├── go.mod
├── helmchart
│   ├── Chart.yaml
│   ├── templates
│   │   ├── NOTES.txt
│   │   ├── _helpers.tpl
│   │   ├── deployment.yaml
│   │   ├── hpa.yaml
│   │   ├── ingress.yaml
│   │   ├── service.yaml
│   │   ├── serviceaccount.yaml
│   │   └── tests
│   │       └── test-connection.yaml
│   └── values.yaml
├── local
│   └── env.sh
├── main.go
├── resources.yaml
└── VERSION
```

### Example Makefile

```Makefile
NAME      ?= simpleserver
PROVIDER  ?= acme.org
GITHUBORG ?= acme
IMAGE     = ghcr.io/$(GITHUBORG)/demo/$(NAME)
COMPONENT = $(PROVIDER)/demo/$(NAME)
OCMREPO   ?= ghcr.io/$(GITHUBORG)/ocm
MULTI     ?= true
PLATFORMS ?= linux/amd64 linux/arm64
REPO_ROOT           = .
VERSION             = $(shell git describe --tags --exact-match 2>/dev/null|| echo "$$(cat $(REPO_ROOT)/VERSION)")
COMMIT              = $(shell git rev-parse HEAD)
EFFECTIVE_VERSION   = $(VERSION)-$(COMMIT)
GIT_TREE_STATE      := $(shell [ -z "$(git status --porcelain 2>/dev/null)" ] && echo clean || echo dirty)
GEN = ./gen
OCM = ocm

CHART_SRCS=$(shell find helmchart -type f)
GO_SRCS=$(shell find . -name \*.go -type f)

ifeq ($(MULTI),true)
FLAGSUF     = .multi
endif

.PHONY: build
build: $(GEN)/build

.PHONY: version
version:
  @echo $(VERSION)

.PHONY: ca
ca: $(GEN)/ca

$(GEN)/ca: $(GEN)/.exists $(GEN)/image.$(NAME)$(FLAGSUF) resources.yaml $(CHART_SRCS)
  $(OCM) create ca -f $(COMPONENT) "$(VERSION)" --provider $(PROVIDER) --file $(GEN)/ca
  $(OCM) add resources --templater spiff $(GEN)/ca COMMIT="$(COMMIT)" VERSION="$(VERSION)" \
    IMAGE="$(IMAGE):$(VERSION)" PLATFORMS="$(PLATFORMS)" MULTI=$(MULTI) resources.yaml
  @touch $(GEN)/ca

$(GEN)/build: $(GO_SRCS)
  go build .
  @touch $(GEN)/build

.PHONY: image
image: $(GEN)/image.$(NAME)

$(GEN)/image.$(NAME): $(GEN)/.exists Dockerfile $(OCMSRCS)
  docker build -t $(IMAGE):$(VERSION) --file Dockerfile $(COMPONENT_ROOT) .;
  @touch $(GEN)/image.$(NAME)

.PHONY: multi
multi: $(GEN)/image.$(NAME).multi

$(GEN)/image.$(NAME).multi: $(GEN)/.exists Dockerfile $(GO_SRCS)
  echo "Building Multi $(PLATFORMS)"
  for i in $(PLATFORMS); do \
  tag=$$(echo $$i | sed -e s:/:-:g); \
  echo "Building platform $$i with tag: $$tag"; \
  docker buildx build --load -t $(IMAGE):$(VERSION)-$$tag --platform $$i .; \
  done
  @touch $(GEN)/image.$(NAME).multi

.PHONY: ctf
ctf: $(GEN)/ctf

$(GEN)/ctf: $(GEN)/ca
  @rm -rf $(GEN)/ctf
  $(OCM) transfer ca $(GEN)/ca $(GEN)/ctf
  touch $(GEN)/ctf

.PHONY: push
push: $(GEN)/ctf $(GEN)/push.$(NAME)

$(GEN)/push.$(NAME): $(GEN)/ctf
  $(OCM) transfer ctf -f $(GEN)/ctf $(OCMREPO)
  @touch $(GEN)/push.$(NAME)

.PHONY: transport
transport:
ifneq ($(TARGETREPO),)
  $(OCM) transfer component -Vc  $(OCMREPO)//$(COMPONENT):$(VERSION) $(TARGETREPO)
else
  @echo "Cannot transport no TARGETREPO defined as destination" && exit 1
endif

$(GEN)/.exists:
  @mkdir -p $(GEN)
  @touch $@

.PHONY: info
info:
  @echo "VERSION:  $(VERSION)"
  @echo "COMMIT:   $(COMMIT)"
  @echo "TREESTATE:   $(GIT_TREE_STATE)"

.PHONY: describe
describe: $(GEN)/ctf
  ocm get resources --lookup $(OCMREPO) -r -o treewide $(GEN)/ctf

.PHONY: descriptor
descriptor: $(GEN)/ctf
  ocm get component -S v3alpha1 -o yaml $(GEN)/ctf

.PHONY: clean
clean:
  rm -rf $(GEN)
```

The Makefile supports the following targets:

- `build` (default) simple Go build
- `version` show current VERSION of Github repository
- `image` build a local Docker image
- `multi` build multi-arch images with Docker's buildx command
- `ca` execute build and create a component archive
- `ctf` create a common transport format archive
- `push` push the common transport archive to an OCI registry
- `info` show variables used in Makefile (version, commit, etc.)
- `describe` display the component version in a tree-form
- `descriptor` show the component descriptor of the component version
- `transport` transport the component from the upload repository into another OCM repository
- `clean` delete all generated files (but does not delete Docker images)

The variables assigned with `?=` at the beginning can be set from outside and override the default
declared in the Makefile. Use either an environment variable or an argument when calling `make`.

Example:

```shell
PROVIDER=foo make ca
```

### Templating the Resources

The Makefile uses a dynamic list of generated platforms for the images. You can just set the `PLATFORMS` variable:

```Makefile
MULTI     ?= true
PLATFORMS ?= linux/amd64 linux/arm64
```

If `MULTI` is set to `true`, the variable `PLATFORMS` will be evaluated to decide which image variants
will be built. This has to be reflected in the `resources.yaml`. It has to use the input type
`dockermulti` and list all the variants which should be packaged into a multi-arch image. This list
depends on the content of the Make variable.

The OCM CLI supports this by enabling templating mechanisms for the content by selecting a templater
using the option `--templater ...`. The example uses the [Spiff templater](https://github.com/mandelsoft/spiff).

```Makefile
$(GEN)/ca: $(GEN)/.exists $(GEN)/image.$(NAME)$(FLAGSUF) resources.yaml $(CHART_SRCS)
  $(OCM) create ca -f $(COMPONENT) "$(VERSION)" --provider $(PROVIDER) --file $(GEN)/ca
  $(OCM) add resources --templater spiff $(GEN)/ca COMMIT="$(COMMIT)" VERSION="$(VERSION)" \
    IMAGE="$(IMAGE):$(VERSION)" PLATFORMS="$(PLATFORMS)" MULTI=$(MULTI) resources.yaml
  @touch $(GEN)/ca
```

The variables given to the `add resources` command are passed to the templater. The template looks
like:

```yaml
name: image
type: ociImage
version: (( values.VERSION ))
input:
  type: (( bool(values.MULTI) ? "dockermulti" :"docker" ))
  repository:  (( index(values.IMAGE, ":") >= 0 ? substr(values.IMAGE,0,index(values.IMAGE,":")) :values.IMAGE ))
  variants: (( bool(values.MULTI) ? map[split(" ", values.PLATFORMS)|v|-> values.IMAGE "-" replace(v,"/","-")] :~~ ))
  path: (( bool(values.MULTI) ? ~~ :values.IMAGE ))
```

By using a variable `values.MULTI`, the command distinguishes between a single Docker image and a multi-arch image.
With `map[]`, the platform list from the Makefile is mapped to a list of tags created by the
`docker buildx` command used in the Makefile. The value `~~` is used to undefine the yaml fields not
required for the selected case (the template can be used for multi- and single-arch builds).

```Makefile
$(GEN)/image.$(NAME).multi: $(GEN)/.exists Dockerfile $(GO_SRCS)
  echo "Building Multi $(PLATFORMS)"
  for i in $(PLATFORMS); do \
  tag=$$(echo $$i | sed -e s:/:-:g); \
  echo "Building platform $$i with tag: $$tag"; \
  docker buildx build --load -t $(IMAGE):$(VERSION)-$$tag --platform $$i .; \
  done
  @touch $(GEN)/image.$(NAME).multi
```

## Pipeline Integration

Pipeline infrastructures are heterogeneous, so there is no universal answer how to
integrate a build pipeline with OCM. Usually, the simplest way is using the OCM command line interface.
Following you will find an example using GitHub actions.

There are two repositories dealing with GitHub actions:
The [first one](https://github.com/open-component-model/ocm-action) provides various actions that can be
called from a workflow. The [second one](https://github.com/open-component-model/ocm-setup-action)
provides the required installation of the OCM CLI into the container.

An typical workflow for a build step will create a component version and a transport archive:

```yaml
jobs:
  create-ocm:
    runs-on: ubuntu-latest
    steps:
      ...
      - name: setup OCM
        uses: open-component-model/ocm-setup-action@main
      ...
      - name: create OCM component version
        uses: open-component-model/ocm-action@main
        with:
          action: create_component
          component: acme.org/demo/simpleserver
          provider: ${{ env.PROVIDER }}
          version: github.com/jensh007
      ...
```

This creates a component version for the current build. Additionally, a CTF archive
can be created or the component version along with the built container images can be uploaded to an
OCI registry, etc.

More documentation is available [here](https://github.com/open-component-model/ocm-action). A full
example can be found in the sample Github repository.

## Static and Dynamic Variable Substitution

Some variables like the `version` or the `commit` change with every build
or release. In many cases, these variables will be auto-generated during the build.

Other variables like the version of 3rd-party components will just change from time to
time and are often set manually by an engineer or release manager. It is useful to separate
between static and dynamic variables. Static files can be checked-in into the source control system and
are maintained manually. Dynamic variables can be generated during the build.

### Example Substitution File

The following example shows how to separate static and dynamic variables.

Static settings, manually maintained:

```yaml
NAME: microblog
COMPONENT_NAME_PREFIX: github.com/acme.org/microblog
PROVIDER: ocm.software
ELASTIC_VERSION: 8.5.1
MARIADB_VERSION: 10.6.11
MARIADB_CHART_VERSION: 11.4.2
NGINX_VERSION: 1.5.1
NGINX_CHART_VERSION: 4.4.2
```

auto-generated from a build script:

```yaml
VERSION: 0.23.1
COMMIT: 5f03021059c7dbe760ac820a014a8a84166ef8b4
```

```shell
ocm add componentversions --create --file ../gen/ctf --settings ../gen/dynamic_settings.yaml --settings static_settings.yaml component-constructor.yaml
```

## Debugging: Explain the Blobs Directory

For analyzing and debugging the content of a CTF archive, there are some supportive commands
to analyze what is contained in the archive and what is stored in which blob:

```shell
tree ../gen/ctf
../gen/ctf
├── artifact-index.json
└── blobs
    ├── ...
    ├── sha256.59ff88331c53a2a94cdd98df58bc6952f056e4b2efc8120095fbc0a870eb0b67
    ├── ...
```

```shell
ocm get resources -r -o wide ../gen/ctf
...
---
REFERENCEPATH: github.com/acme.org/microblog/nginx-controller:1.5.1
NAME         : nginx-controller-chart
VERSION      : 1.5.1
IDENTITY     :
TYPE         : helmChart
RELATION     : local
ACCESSTYPE   : localBlob
ACCESSSPEC   : {"localReference":"sha256:59ff88331c53a2a94cdd98df58bc6952f056e4b2efc8120095fbc0a870eb0b67","mediaType":"application/vnd.oci.image.manifest.v1+tar+gzip","referenceName":"github.com/acme.org/microblog/nginx-controller/ingress-nginx:4.4.2"}
...
```

## Self-Contained Transport Archives

The transport archive created from a component constructor file, using the command `ocm add  componentversions --create ...`, does not automatically resolve image references to external OCI registries and stores them in the archive. If you want to create a self-contained transport archive with all images stored as local artifacts, you need to use the `--copy-resources` option in the `ocm transfer ctf` command. This will copy all external images to the blobs directory of the archive.

```shell
ocm transfer ctf --copy-resources <ctf-dir> <new-ctf-dir-or-oci-repo-url>
```

Note that this archive can become huge, depending on the size of the external images !

## CICD Integration

Configure rarely changing variables in a static file and generate dynamic variables
during the build from the environment. See the [Static and Dynamic Variable Substitution](#static-and-dynamic-variable-substitution) section above.
