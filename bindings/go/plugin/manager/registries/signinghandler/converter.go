package signinghandler

import (
	"context"
	"fmt"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts/signing/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
)

type pluginConverter struct {
	externalPlugin v1.SignatureHandlerContract[runtime.Typed]
	scheme         *runtime.Scheme
}

func (r *pluginConverter) GetSigningCredentialConsumerIdentity(ctx context.Context, name string, unsigned descriptor.Digest, config runtime.Typed) (identity runtime.Identity, err error) {
	request := &v1.GetSignerIdentityRequest[runtime.Typed]{
		SignRequest: v1.SignRequest[runtime.Typed]{
			Digest: descriptor.ConvertToV2Digest(&unsigned),
			Config: config,
		},
		Name: name,
	}

	result, err := r.externalPlugin.GetSignerIdentity(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to get identity: %w", err)
	}

	return result.Identity, nil
}

func (r *pluginConverter) Sign(ctx context.Context, unsigned descriptor.Digest, config runtime.Typed, credentials map[string]string) (signed descriptor.SignatureInfo, err error) {
	v2digest := descriptor.ConvertToV2Digest(&unsigned)

	request := &v1.SignRequest[runtime.Typed]{
		Digest: v2digest,
		Config: config,
	}
	result, err := r.externalPlugin.Sign(ctx, request, credentials)
	if err != nil {
		return descriptor.SignatureInfo{}, err
	}
	signatureInfo := *descriptor.ConvertFromV2SignatureInfo(result.Signature)

	return signatureInfo, nil
}

func (r *pluginConverter) GetVerifyingCredentialConsumerIdentity(ctx context.Context, signed descriptor.Signature, config runtime.Typed) (identity runtime.Identity, err error) {
	request := &v1.GetVerifierIdentityRequest[runtime.Typed]{
		VerifyRequest: v1.VerifyRequest[runtime.Typed]{
			Signature: descriptor.ConvertToV2Signature(&signed),
			Config:    config,
		},
	}

	result, err := r.externalPlugin.GetVerifierIdentity(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to get identity: %w", err)
	}

	return result.Identity, nil
}

func (r *pluginConverter) Verify(ctx context.Context, signed descriptor.Signature, config runtime.Typed, credentials map[string]string) error {
	v2signature := descriptor.ConvertToV2Signature(&signed)

	request := &v1.VerifyRequest[runtime.Typed]{
		Signature: v2signature,
		Config:    config,
	}
	// VerifyResponse is empty right now
	_, err := r.externalPlugin.Verify(ctx, request, credentials)
	return err
}

var _ signing.Handler = (*pluginConverter)(nil)

func (r *SigningRegistry) externalPluginConverter(plugin v1.SignatureHandlerContract[runtime.Typed], scheme *runtime.Scheme) *pluginConverter {
	return &pluginConverter{
		externalPlugin: plugin,
		scheme:         scheme,
	}
}
