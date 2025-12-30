package main

import (
	"encoding/json"
	"os"

	"github.com/extism/go-pdk"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/input/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

//go:wasmexport process_resource
func ProcessResource() int32 {
	input := pdk.Input()

	var request *v1.ProcessSourceInputRequest
	if err := json.Unmarshal(input, &request); err != nil {
		pdk.SetError(err)
		return 1
	}

	response := v1.ProcessResourceInputResponse{
		Resource: &descriptorv2.Resource{
			ElementMeta: descriptorv2.ElementMeta{
				ObjectMeta: descriptorv2.ObjectMeta{
					Name:    "test-resource",
					Version: "v0.0.1",
				},
			},
		},
		Location: &types.Location{
			LocationType: "localFile",
			Value:        "/tmp/wasm-resource-file",
		},
	}

	output, err := json.Marshal(response)
	if err != nil {
		pdk.SetError(err)
		return 1
	}

	pdk.Output(output)
	return 0
}

//go:wasmexport process_source
func ProcessSource() int32 {
	input := pdk.Input()

	var request v1.ProcessSourceInputRequest
	if err := json.Unmarshal(input, &request); err != nil {
		pdk.SetError(err)
		return 1
	}

	response := v1.ProcessSourceInputResponse{
		Source: &descriptorv2.Source{
			ElementMeta: descriptorv2.ElementMeta{
				ObjectMeta: descriptorv2.ObjectMeta{
					Name:    "test-source",
					Version: "v0.0.1",
				},
			},
		},
		Location: &types.Location{
			LocationType: "localFile",
			Value:        "/tmp/wasm-source-file",
		},
	}

	output, err := json.Marshal(response)
	if err != nil {
		pdk.SetError(err)
		return 1
	}

	pdk.Output(output)
	return 0
}

//go:wasmexport capabilities
func Capabilities() int32 {
	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	capabilities := endpoints.NewEndpoints(scheme)
	content, err := capabilities.MarshalJSON()
	if err != nil {
		pdk.SetError(err)
		os.Exit(1)
	}

	output, err := json.Marshal(content)
	if err != nil {
		pdk.SetError(err)
		return 1
	}

	pdk.Output(output)
	return 0
}

func main() {}
