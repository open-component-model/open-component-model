package input

import (
	"context"
	"os"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/input/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type sourceInputPluginConverter struct {
	externalPlugin v1.SourceInputPluginContract
	scheme         *runtime.Scheme
}

func (r *sourceInputPluginConverter) GetCredentialConsumerIdentity(ctx context.Context, source *constructorruntime.Source) (runtime.Identity, error) {
	result, err := r.externalPlugin.GetIdentity(ctx, &v1.GetIdentityRequest[runtime.Typed]{
		// TODO: Is this the right type?
		Typ: source.Access,
	})
	if err != nil {
		return nil, err
	}

	return result.Identity, nil
}

func (r *sourceInputPluginConverter) ProcessSource(ctx context.Context, source *constructorruntime.Source, credentials map[string]string) (*constructor.SourceInputMethodResult, error) {
	var labels []constructorv1.Label
	for _, v := range source.Labels {
		labels = append(labels, constructorv1.Label{
			Name:    v.Name,
			Value:   v.Value,
			Signing: v.Signing,
		})
	}
	request := &v1.ProcessSourceInputRequest{
		Source: &constructorv1.Source{
			ElementMeta: constructorv1.ElementMeta{
				ObjectMeta: constructorv1.ObjectMeta{
					Name:    source.Name,
					Version: source.Version,
					Labels:  labels,
				},
				ExtraIdentity: source.ExtraIdentity,
			},
			Type: source.Type,
		},
	}

	var raw runtime.Raw
	if err := r.scheme.Convert(source.Access, &raw); err == nil {
		request.Source.Access = &raw
	}
	if err := r.scheme.Convert(source.Input, &raw); err == nil {
		request.Source.Input = &raw
	}

	// translate to the right thing that has NO JSON stuff in it.
	result, err := r.externalPlugin.ProcessSource(ctx, request, credentials)
	if err != nil {
		return nil, err
	}

	converted := constructorruntime.ConvertToRuntimeSource(result.Source)
	var rBlob blob.ReadOnlyBlob
	if result.Location.LocationType == types.LocationTypeLocalFile {
		file, err := os.Open(result.Location.Value)
		if err != nil {
			return nil, err
		}

		rBlob = blob.NewDirectReadOnlyBlob(file)
	}

	sourceInputMethodResult := &constructor.SourceInputMethodResult{
		ProcessedSource:   &converted,
		ProcessedBlobData: rBlob,
	}

	return sourceInputMethodResult, nil
}

func (r *RepositoryRegistry) externalToSourceInputPluginConverter(plugin v1.SourceInputPluginContract, scheme *runtime.Scheme) *sourceInputPluginConverter {
	return &sourceInputPluginConverter{
		externalPlugin: plugin,
		scheme:         scheme,
	}
}
