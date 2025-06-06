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

type resourceInputPluginConverter struct {
	externalPlugin v1.ResourceInputPluginContract
	scheme         *runtime.Scheme
}

func (r *resourceInputPluginConverter) GetCredentialConsumerIdentity(ctx context.Context, resource *constructorruntime.Resource) (runtime.Identity, error) {
	// figure out the type
	result, err := r.externalPlugin.GetIdentity(ctx, &v1.GetIdentityRequest[runtime.Typed]{
		// TODO: Is this the right type?
		Typ: resource.Access,
	})
	if err != nil {
		return nil, err
	}

	return result.Identity, nil
}

func (r *resourceInputPluginConverter) ProcessResource(ctx context.Context, resource *constructorruntime.Resource, credentials map[string]string) (*constructor.ResourceInputMethodResult, error) {
	var labels []constructorv1.Label
	for _, v := range resource.Labels {
		labels = append(labels, constructorv1.Label{
			Name:    v.Name,
			Value:   v.Value,
			Signing: v.Signing,
		})
	}
	var sourceRefs []constructorv1.SourceRef
	for _, v := range resource.SourceRefs {
		var sourceLabels []constructorv1.Label
		for _, l := range v.Labels {
			sourceLabels = append(sourceLabels, constructorv1.Label{
				Name:    l.Name,
				Value:   l.Value,
				Signing: l.Signing,
			})
		}
		sourceRefs = append(sourceRefs, constructorv1.SourceRef{
			IdentitySelector: v.IdentitySelector,
			Labels:           sourceLabels,
		})
	}

	request := &v1.ProcessResourceInputRequest{
		Resource: &constructorv1.Resource{
			ElementMeta: constructorv1.ElementMeta{
				ObjectMeta: constructorv1.ObjectMeta{
					Name:    resource.Name,
					Version: resource.Version,
					Labels:  labels,
				},
				ExtraIdentity: resource.ExtraIdentity,
			},
			SourceRefs: sourceRefs,
			Type:       resource.Type,
			Relation:   constructorv1.ResourceRelation(resource.Relation),
		},
	}

	var raw runtime.Raw
	if err := r.scheme.Convert(resource.Access, &raw); err == nil {
		request.Resource.Access = &raw
	}
	if err := r.scheme.Convert(resource.Input, &raw); err == nil {
		request.Resource.Input = &raw
	}

	// translate to the right thing that has NO JSON stuff in it.
	result, err := r.externalPlugin.ProcessResource(ctx, request, credentials)
	if err != nil {
		return nil, err
	}

	converted := constructorruntime.ConvertToRuntimeResource(result.Resource)
	var rBlob blob.ReadOnlyBlob
	if result.Location.LocationType == types.LocationTypeLocalFile {
		file, err := os.Open(result.Location.Value)
		if err != nil {
			return nil, err
		}

		rBlob = blob.NewDirectReadOnlyBlob(file)
	}

	resourceInputMethodResult := &constructor.ResourceInputMethodResult{
		ProcessedResource: &converted,
		ProcessedBlobData: rBlob,
	}

	return resourceInputMethodResult, nil
}

func (r *RepositoryRegistry) externalToResourceInputPluginConverter(plugin v1.ResourceInputPluginContract, scheme *runtime.Scheme) *resourceInputPluginConverter {
	return &resourceInputPluginConverter{
		externalPlugin: plugin,
		scheme:         scheme,
	}
}
