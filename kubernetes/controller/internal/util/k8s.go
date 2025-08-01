package util

import (
	"context"
	"fmt"

	"github.com/fluxcd/pkg/runtime/conditions"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"
)

type NotReadyError struct {
	objectName string
}

func (e NotReadyError) Error() string {
	return fmt.Sprintf("object is not ready: %s", e.objectName)
}

type DeletionError struct {
	objectName string
}

func (e DeletionError) Error() string {
	return fmt.Sprintf("object is being deleted: %s", e.objectName)
}

type Getter interface {
	GetConditions() []metav1.Condition
	ctrl.Object
}

type ObjectPointerType[T any] interface {
	*T
	Getter
}

func GetNamespaced[T any, P ObjectPointerType[T]](ctx context.Context, client ctrl.Reader, key v1.LocalObjectReference, namespace string) (P, error) {
	obj := P(new(T))
	if err := client.Get(ctx, ctrl.ObjectKey{Namespace: namespace, Name: key.Name}, obj); err != nil {
		return nil, fmt.Errorf("failed to locate object: %w", err)
	}

	return obj, nil
}

func Get[T any, P ObjectPointerType[T]](ctx context.Context, client ctrl.Reader, key ctrl.ObjectKey) (P, error) {
	obj := P(new(T))
	if err := client.Get(ctx, key, obj); err != nil {
		return nil, fmt.Errorf("failed to locate object: %w", err)
	}

	return obj, nil
}

func GetReadyObject[T any, P ObjectPointerType[T]](ctx context.Context, client ctrl.Reader, key ctrl.ObjectKey) (P, error) {
	obj := P(new(T))
	if err := client.Get(ctx, key, obj); err != nil {
		return nil, fmt.Errorf("failed to locate object: %w", err)
	}

	if !obj.GetDeletionTimestamp().IsZero() {
		return nil, DeletionError{key.String()}
	}

	if !conditions.IsReady(obj) {
		return nil, NotReadyError{key.String()}
	}

	return obj, nil
}
