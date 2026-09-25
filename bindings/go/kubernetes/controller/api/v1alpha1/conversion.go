package v1alpha1

import (
	ctrl "sigs.k8s.io/controller-runtime"
)

func (*Component) Hub()  {}
func (*Deployer) Hub()   {}
func (*Repository) Hub() {}
func (*Resource) Hub()   {}

func (r *Component) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, r).Complete()
}

func (r *Deployer) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, r).Complete()
}

func (r *Repository) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, r).Complete()
}

func (r *Resource) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, r).Complete()
}
