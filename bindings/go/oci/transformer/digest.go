package transformer

import (
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
)

// dropInjectedDigest removes a digest the add/upload sink filled in for a resource whose
// source carried none.
//
// The sink (pack/blob) back-fills a *missing* OCM resource digest from the content. For a
// resource whose source descriptor had no digest that injection would change the signed,
// normalized descriptor and break a signature that deliberately excluded the resource, so
// transfer drops it again. A digest the source did carry is left as the sink returned it:
// the sink verifies a complete digest against the content and completes an incomplete one,
// so its result is authoritative and must survive the transfer.
func dropInjectedDigest(src, out *v2.Resource) {
	if src.Digest == nil {
		out.Digest = nil
	}
}
