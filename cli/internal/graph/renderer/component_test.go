package displaymanager

import (
	_ "net/http/pprof"
)

//const (
//	AttributeComponentIdentity   = "attributes.ocm.software/component-identity"
//	AttributeComponentDescriptor = "attributes.ocm.software/component-descriptor"
//)
//
//func Test_RecursivelyGetComponents(t *testing.T) {
//	r := require.New(t)
//
//	ctx := context.Background()
//
//	components := []descriptor.Component{
//		{
//			ComponentMeta: descriptor.ComponentMeta{
//				ObjectMeta: descriptor.ObjectMeta{
//					Name:    "rootID",
//					Version: "1.0.0",
//				},
//			},
//			References: []descriptor.Reference{
//				{
//					Component: "leaf-b",
//					ElementMeta: descriptor.ElementMeta{
//						ObjectMeta: descriptor.ObjectMeta{
//							Name:    "leaf-b",
//							Version: "1.0.0",
//						},
//					},
//				},
//				{
//					Component: "leaf-a",
//					ElementMeta: descriptor.ElementMeta{
//						ObjectMeta: descriptor.ObjectMeta{
//							Name:    "leaf-a",
//							Version: "1.0.0",
//						},
//					},
//				},
//			},
//		},
//		{
//			ComponentMeta: descriptor.ComponentMeta{
//				ObjectMeta: descriptor.ObjectMeta{
//					Name:    "leaf-a",
//					Version: "1.0.0",
//				},
//			},
//			References: []descriptor.Reference{
//				{
//					Component: "leaf-b",
//					ElementMeta: descriptor.ElementMeta{
//						ObjectMeta: descriptor.ObjectMeta{
//							Name:    "leaf-b",
//							Version: "1.0.0",
//						},
//					},
//				},
//			},
//		},
//		{
//			ComponentMeta: descriptor.ComponentMeta{
//				ObjectMeta: descriptor.ObjectMeta{
//					Name:    "leaf-b",
//					Version: "1.0.0",
//				},
//			},
//			References: []descriptor.Reference{
//				{
//					Component: "leaf-c",
//					ElementMeta: descriptor.ElementMeta{
//						ObjectMeta: descriptor.ObjectMeta{
//							Name:    "leaf-c",
//							Version: "1.0.0",
//						},
//					},
//				},
//			},
//		},
//		{
//			ComponentMeta: descriptor.ComponentMeta{
//				ObjectMeta: descriptor.ObjectMeta{
//					Name:    "leaf-c",
//					Version: "1.0.0",
//				},
//			},
//		},
//	}
//
//	t.Run("recursively discover a component with all its references", func(t *testing.T) {
//		resolver := &MapComponentVersionRepository{
//			repo: map[string]*descriptor.Descriptor{},
//		}
//		for _, c := range components {
//			r.NoError(resolver.AddComponentVersion(ctx, &descriptor.Descriptor{
//				Component: c,
//			}))
//		}
//		//dag := NewGraph(resolver)
//		mydag := syncdag.NewDirectedAcyclicGraph[string]()
//		f := func(v *syncdag.Vertex[string]) string {
//			// Parse the identity string to extract name and version
//			parts := strings.Split(v.ID, ",")
//			if len(parts) >= 2 {
//				name := strings.TrimPrefix(parts[0], "\"name\":\"")
//				version := strings.TrimSuffix(parts[1], "\"}")
//				version = strings.TrimPrefix(version, "version\":\"")
//
//				return fmt.Sprintf("%s:%s", name, version)
//				//state, _ := v.Attributes.Load(AttributeDiscoveryState)
//				//switch state {
//				//case StateDiscovering:
//				//	return fmt.Sprintf("%s:%s (discovering...)", name, version)
//				//case StateError:
//				//	return fmt.Sprintf("%s:%s (error)", name, version)
//				//case StateDiscovered:
//				//	return fmt.Sprintf("%s:%s (discovered)", name, version)
//				//case StateCompleted:
//				//	return fmt.Sprintf("%s:%s (completed)", name, version)
//				//default:
//				//	return fmt.Sprintf("%s:%s (pending)", name, version)
//				//}
//			}
//			return v.ID
//		}
//
//		treeDisplayManager := NewTreeRenderer(mydag, f, WithMode(ModeLive))
//		treeDisplayManager.Start(ctx, components[0].ToIdentity().String())
//		r.NoError(mydag.Traverse(ctx, &syncdag.Vertex[string]{
//			ID: components[0].ToIdentity().String(),
//			Attributes: func() *sync.Map {
//				m := &sync.Map{}
//				m.Store(AttributeComponentIdentity, components[0].ToIdentity())
//				return m
//			}(),
//		}, func(ctx context.Context, v *syncdag.Vertex[string]) ([]*syncdag.Vertex[string], error) {
//			untypedIdentity, _ := v.Attributes.Load(AttributeComponentIdentity)
//			id := untypedIdentity.(runtime.Identity)
//
//			desc, err := resolver.GetComponentVersion(ctx, id["name"], id["version"])
//			if err != nil {
//				return nil, fmt.Errorf("failed to get component descriptor for identity %q: %w", v.ID, err)
//			}
//			neighbors := make([]*syncdag.Vertex[string], len(desc.Component.References))
//			for index, ref := range desc.Component.References {
//				neighbors[index] = &syncdag.Vertex[string]{
//					ID: runtime.Identity{
//						"name":    ref.Component,
//						"version": ref.Version,
//					}.String(),
//					Attributes: func() *sync.Map {
//						m := &sync.Map{}
//						m.Store(AttributeComponentIdentity, ref.ToIdentity())
//						m.Store(AttributeComponentDescriptor, desc)
//						return m
//					}(),
//				}
//			}
//			return neighbors, nil
//		}))
//		r.NoError(treeDisplayManager.Wait(ctx))
//	})
//}
//
//type MapComponentVersionRepository struct {
//	repo map[string]*descriptor.Descriptor
//}
//
//func (m *MapComponentVersionRepository) AddComponentVersion(_ context.Context, desc *descriptor.Descriptor) error {
//	m.repo[desc.Component.ToIdentity().String()] = desc
//	return nil
//}
//
//func (m *MapComponentVersionRepository) GetComponentVersion(_ context.Context, component, version string) (*descriptor.Descriptor, error) {
//	identity := runtime.Identity{
//		"name":    component,
//		"version": version,
//	}
//	if desc, ok := m.repo[identity.String()]; ok {
//		return desc, nil
//	}
//	return nil, fmt.Errorf("no descriptor found for identity %q", identity)
//}
//
//func (m *MapComponentVersionRepository) ListComponentVersions(ctx context.Context, component string) ([]string, error) {
//	//TODO implement me
//	panic("implement me")
//}
//
//func (m *MapComponentVersionRepository) AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (*descriptor.Resource, error) {
//	//TODO implement me
//	panic("implement me")
//}
//
//func (m *MapComponentVersionRepository) GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
//	//TODO implement me
//	panic("implement me")
//}
//
//func (m *MapComponentVersionRepository) AddLocalSource(ctx context.Context, component, version string, src *descriptor.Source, content blob.ReadOnlyBlob) (*descriptor.Source, error) {
//	//TODO implement me
//	panic("implement me")
//}
//
//func (m *MapComponentVersionRepository) GetLocalSource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Source, error) {
//	//TODO implement me
//	panic("implement me")
//}
