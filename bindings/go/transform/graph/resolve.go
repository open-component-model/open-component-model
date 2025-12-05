package graph

//import (
//	"fmt"
//	"strings"
//
//	"ocm.software/open-component-model/bindings/go/cel/fieldpath"
//)
//
//func GetValueFromPath(object map[string]any, path string) (interface{}, error) {
//	path = strings.TrimPrefix(path, ".") // Remove leading dot if present
//	segments, err := fieldpath.Parse(path)
//	if err != nil {
//		return nil, fmt.Errorf("invalid path '%s': %v", path, err)
//	}
//
//	current := interface{}(object)
//
//	for _, segment := range segments {
//		if segment.Index >= 0 {
//			// Handle array access
//			array, ok := current.([]interface{})
//			if !ok {
//				return nil, fmt.Errorf("expected array at path segment: %v", segment)
//			}
//
//			if segment.Index >= len(array) {
//				return nil, fmt.Errorf("array index out of bounds: %d", segment.Index)
//			}
//
//			current = array[segment.Index]
//		} else {
//			// Handle object access
//			currentMap, ok := current.(map[string]interface{})
//			if !ok {
//				return nil, fmt.Errorf("expected map at path segment: %v", segment)
//			}
//
//			value, ok := currentMap[segment.Name]
//			if !ok {
//				return nil, fmt.Errorf("key not found: %s", segment.Name)
//			}
//			current = value
//		}
//	}
//
//	return current, nil
//}
