package controller

import (
	halkyon "halkyon.io/api/component/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
)

type Component struct {
	*halkyon.Component
	requeue bool
}

func (in *Component) SetAPIObject(object runtime.Object) {
	in.Component = object.(*halkyon.Component)
}

func (in *Component) GetAPIObject() runtime.Object {
	return in.Component
}

func (in *Component) Clone() Resource {
	component := NewComponent(in.Component)
	component.requeue = in.requeue
	return component
}

func NewComponent(component ...*halkyon.Component) *Component {
	c := &halkyon.Component{}
	if component != nil {
		c = component[0]
	}
	return &Component{
		Component: c,
		requeue:   false,
	}
}

func (in *Component) SetNeedsRequeue(requeue bool) {
	in.requeue = in.requeue || requeue
}

func (in *Component) NeedsRequeue() bool {
	return in.requeue
}

func (in *Component) isPending() bool {
	status := halkyon.ComponentPending
	if halkyon.BuildDeploymentMode == in.Spec.DeploymentMode {
		status = halkyon.ComponentBuilding
	}
	return status == in.Status.Phase
}

func (in *Component) SetInitialStatus(msg string) bool {
	if !in.isPending() || in.Status.Message != msg {
		in.Status.Phase = halkyon.ComponentPending
		if halkyon.BuildDeploymentMode == in.Spec.DeploymentMode {
			in.Status.Phase = halkyon.ComponentBuilding
		}
		in.Status.Message = msg
		in.SetNeedsRequeue(true)
		return true
	}
	return false
}

func (in *Component) IsValid() bool {
	return true // todo: implement me
}

func (in *Component) SetErrorStatus(err error) bool {
	errMsg := err.Error()
	if halkyon.ComponentFailed != in.Status.Phase || errMsg != in.Status.Message {
		in.Status.Phase = halkyon.ComponentFailed
		in.Status.Message = errMsg
		in.SetNeedsRequeue(true)
		return true
	}
	return false
}

func (in *Component) SetSuccessStatus(dependentName, msg string) bool {
	if dependentName != in.Status.PodName || halkyon.ComponentReady != in.Status.Phase || msg != in.Status.Message {
		in.Status.Phase = halkyon.ComponentReady
		in.Status.PodName = dependentName
		in.Status.Message = msg
		in.requeue = false
		return true
	}
	return false
}

func (in *Component) GetStatusAsString() string {
	return in.Status.Phase.String()
}

func (in *Component) ShouldDelete() bool {
	return true
}
