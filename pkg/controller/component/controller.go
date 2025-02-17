/*
Licensed to the Apache Software Foundation (ASF) under one or more
contributor license agreements.  See the NOTICE file distributed with
this work for additional information regarding copyright ownership.
The ASF licenses this file to You under the Apache License, Version 2.0
(the "License"); you may not use this file except in compliance with
the License.  You may obtain a copy of the License at

   http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package component

import (
	"context"
	"github.com/knative/pkg/apis"
	taskRunv1alpha1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1alpha1"
	component "halkyon.io/api/component/v1beta1"
	"halkyon.io/api/v1beta1"
	controller2 "halkyon.io/operator/pkg/controller"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// newReconciler returns a new reconcile.Reconciler
func NewComponentReconciler(mgr manager.Manager) *ReconcileComponent {
	// todo: make this configurable
	images := make(map[string]imageInfo, 7)
	defaultEnvVar := make(map[string]string, 7)
	// defaultEnvVar["JAVA_APP_JAR"] = "app.jar"
	images["spring-boot"] = imageInfo{
		registryRef: "quay.io/halkyonio/spring-boot-s2i",
		defaultEnv:  defaultEnvVar,
	}
	images["vert.x"] = imageInfo{
		registryRef: "quay.io/halkyonio/openjdk8-s2i",
		defaultEnv:  defaultEnvVar,
	}
	images["thorntail"] = imageInfo{
		registryRef: "quay.io/halkyonio/openjdk8-s2i",
		defaultEnv:  defaultEnvVar,
	}
	// References images
	images["openjdk8"] = imageInfo{registryRef: "registry.access.redhat.com/redhat-openjdk-18/openjdk18-openshift"}
	images["nodejs"] = imageInfo{registryRef: "nodeshift/centos7-s2i-nodejs"}
	images[supervisorImageId] = imageInfo{registryRef: "quay.io/halkyonio/supervisord"}

	supervisor := component.Component{
		ObjectMeta: v1.ObjectMeta{
			Name: supervisorContainerName,
		},
		Spec: component.ComponentSpec{
			Runtime: supervisorImageId,
			Version: latestVersionTag,
			Envs: []v1beta1.Env{
				{
					Name: "CMDS",
					Value: "run-java:/usr/local/s2i/run;run-node:/usr/libexec/s2i/run;compile-java:/usr/local/s2i" +
						"/assemble;build:/deployments/buildapp",
				},
			},
		},
	}

	baseReconciler := controller2.NewBaseGenericReconciler(controller2.NewComponent(), mgr)
	r := &ReconcileComponent{
		BaseGenericReconciler: baseReconciler,
		runtimeImages:         images,
		supervisor:            &supervisor,
	}
	baseReconciler.SetReconcilerFactory(r)

	r.AddDependentResource(newPvc())
	r.AddDependentResource(newDeployment(r))
	r.AddDependentResource(newService(r))
	r.AddDependentResource(newServiceAccount())
	r.AddDependentResource(newRoute(r))
	r.AddDependentResource(newIngress(r))
	r.AddDependentResource(newTask())
	r.AddDependentResource(newTaskRun(r))
	r.AddDependentResource(controller2.NewRole())
	r.AddDependentResource(controller2.NewRoleBinding())
	return r
}

type imageInfo struct {
	registryRef string
	defaultEnv  map[string]string
}

type ReconcileComponent struct {
	*controller2.BaseGenericReconciler
	runtimeImages map[string]imageInfo
	supervisor    *component.Component
}

func (ReconcileComponent) asComponent(object runtime.Object) *controller2.Component {
	return object.(*controller2.Component)
}

func (r *ReconcileComponent) IsDependentResourceReady(resource controller2.Resource) (depOrTypeName string, ready bool) {
	c := r.asComponent(resource)
	if component.BuildDeploymentMode == c.Spec.DeploymentMode {
		taskRun, err := r.MustGetDependentResourceFor(resource, &taskRunv1alpha1.TaskRun{}).Fetch(r.Helper())
		if err != nil || !r.isBuildSucceed(taskRun.(*taskRunv1alpha1.TaskRun)) {
			return "taskRun job", false
		}
		return taskRun.(*taskRunv1alpha1.TaskRun).Name, true
	} else {
		pod, err := r.fetchPod(c)
		if err != nil || !r.isPodReady(pod) {
			return "pod", false
		}
		return pod.Name, true
	}
}

func (r *ReconcileComponent) CreateOrUpdate(object controller2.Resource) (err error) {
	c := r.asComponent(object)
	if component.BuildDeploymentMode == c.Spec.DeploymentMode {
		err = r.installBuildMode(c, c.Namespace)
	} else {
		err = r.installDevMode(c, c.Namespace)
	}
	return err
}

func (r *ReconcileComponent) Delete(resource controller2.Resource) error {
	if r.IsTargetClusterRunningOpenShift() {
		// Delete the ImageStream created by OpenShift if it exists as the Component doesn't own this resource
		// when it is created during build deployment mode
		imageStream := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "image.openshift.io/v1",
				"kind":       "ImageStream",
				"metadata": map[string]interface{}{
					"name":      resource.GetName(), // todo: is this correct? we should determine the name based on the component
					"namespace": resource.GetNamespace(),
				},
			},
		}

		// attempt to delete the imagestream if it exists
		if e := r.Client.Delete(context.TODO(), imageStream); e != nil && !errors.IsNotFound(e) {
			return e
		}
	}
	return nil
}

// Check if the Pod Condition is Type = Ready and Status = True
func (r *ReconcileComponent) isPodReady(pod *corev1.Pod) bool {
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// Check if the TaskRun Condition is Type = SUCCEEDED and Status = True
func (r *ReconcileComponent) isBuildSucceed(taskRun *taskRunv1alpha1.TaskRun) bool {
	for _, c := range taskRun.Status.Conditions {
		if c.Type == apis.ConditionSucceeded && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}
