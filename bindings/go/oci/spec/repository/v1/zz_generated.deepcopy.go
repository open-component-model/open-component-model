//go:build !ignore_autogenerated
// +build !ignore_autogenerated

// Code generated by deepcopy-gen-v0.32. DO NOT EDIT.

package v1

import (
	runtime "ocm.software/open-component-model/bindings/go/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *OCIRepository) DeepCopyInto(out *OCIRepository) {
	*out = *in
	out.Type = in.Type
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new OCIRepository.
func (in *OCIRepository) DeepCopy() *OCIRepository {
	if in == nil {
		return nil
	}
	out := new(OCIRepository)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyTyped is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Typed.
func (in *OCIRepository) DeepCopyTyped() runtime.Typed {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}
