package applyset

import (
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"
)

func (a *applySet) desiredParentAnnotations(useSuperset bool) (map[string]string, sets.Set[string], sets.Set[string]) {
	annotations := make(map[string]string)

	// Generate sorted comma-separated list of GKs
	gks := sets.New[string]()
	for gk := range a.desiredRESTMappings {
		gks.Insert(gk.String())
	}

	if useSuperset {
		// Include current GKs from annotations
		for _, gk := range strings.Split(a.currentAnnotations[ApplySetGKsAnnotation], ",") {
			gk = strings.TrimSpace(gk)
			if gk != "" {
				gks.Insert(gk)
			}
		}
	}

	gksList := gks.UnsortedList()
	sort.Strings(gksList)
	annotations[ApplySetGKsAnnotation] = strings.Join(gksList, ",")

	// Generate sorted comma-separated list of namespaces
	nss := a.desiredNamespaces.Clone()

	if useSuperset {
		// Include current namespaces from annotations
		for _, ns := range strings.Split(a.currentAnnotations[ApplySetAdditionalNamespacesAnnotation], ",") {
			ns = strings.TrimSpace(ns)
			if ns != "" {
				nss.Insert(ns)
			}
		}
	}

	// Remove the parent's namespace from the list
	if a.parent.GetNamespace() != "" {
		nss.Delete(a.parent.GetNamespace())
	}

	if len(nss) > 0 {
		nsList := nss.UnsortedList()
		sort.Strings(nsList)
		annotations[ApplySetAdditionalNamespacesAnnotation] = strings.Join(nsList, ",")
	}

	return annotations, nss, gks
}
