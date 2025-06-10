package digestprocessor

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/constructor"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts/digestprocessor/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type resourceDigestProcessorPluginConverter struct {
	externalPlugin v1.ResourceDigestProcessorContract
	scheme         *runtime.Scheme
}

func (r *resourceDigestProcessorPluginConverter) ProcessResourceDigest(ctx context.Context, resource *descriptor.Resource) (*descriptor.Resource, error) {
	var labels []descriptorv2.Label
	for _, v := range resource.Labels {
		labels = append(labels, descriptorv2.Label{
			Name:    v.Name,
			Value:   v.Value,
			Signing: v.Signing,
		})
	}
	var sourceRefs []descriptorv2.SourceRef
	for _, v := range resource.SourceRefs {
		var sourceLabels []descriptorv2.Label
		for _, l := range v.Labels {
			sourceLabels = append(sourceLabels, descriptorv2.Label{
				Name:    l.Name,
				Value:   l.Value,
				Signing: l.Signing,
			})
		}
		sourceRefs = append(sourceRefs, descriptorv2.SourceRef{
			IdentitySelector: v.IdentitySelector,
			Labels:           sourceLabels,
		})
	}
	convertResource := &descriptorv2.Resource{
		ElementMeta: descriptorv2.ElementMeta{
			ObjectMeta: descriptorv2.ObjectMeta{
				Name:    resource.Name,
				Version: resource.Version,
				Labels:  labels,
			},
			ExtraIdentity: resource.ExtraIdentity,
		},
		SourceRefs: sourceRefs,
		Type:       resource.Type,
		Relation:   descriptorv2.ResourceRelation(resource.Relation),
		Size:       resource.Size,
	}
	if resource.Digest != nil {
		digest := &descriptorv2.Digest{
			HashAlgorithm:          resource.Digest.HashAlgorithm,
			NormalisationAlgorithm: resource.Digest.NormalisationAlgorithm,
			Value:                  resource.Digest.Value,
		}
		convertResource.Digest = digest
	}

	var raw runtime.Raw
	if err := r.scheme.Convert(resource.Access, &raw); err == nil {
		convertResource.Access = &raw
	}

	request := &v1.ProcessResourceDigestRequest{
		Resource: convertResource,
	}
	response, err := r.externalPlugin.ProcessResourceDigest(ctx, request, map[string]string{})
	if err != nil {
		return nil, fmt.Errorf("failed to process resource digest: %w", err)
	}

	convert := descriptor.ConvertFromV2Resources([]descriptorv2.Resource{*response.Resource})
	return &convert[0], nil
}

var _ constructor.ResourceDigestProcessor = (*resourceDigestProcessorPluginConverter)(nil)

func (r *RepositoryRegistry) externalToResourceDigestProcessorPluginConverter(plugin v1.ResourceDigestProcessorContract, scheme *runtime.Scheme) *resourceDigestProcessorPluginConverter {
	return &resourceDigestProcessorPluginConverter{
		externalPlugin: plugin,
		scheme:         scheme,
	}
}
