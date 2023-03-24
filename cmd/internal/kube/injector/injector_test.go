package injector

import (
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"testing"
)

func Test_Inject(t *testing.T) {
	toUnstructured := func(obj runtime.Object) *unstructured.Unstructured {
		u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&obj)
		if err != nil {
			panic(err)
		}

		return &unstructured.Unstructured{Object: u}
	}

	appendContainer := func(deployment *appsv1.Deployment, container v1.Container) *appsv1.Deployment {
		injectedDeployment := deployment.DeepCopy()
		containers := injectedDeployment.Spec.Template.Spec.Containers
		injectedDeployment.Spec.Template.Spec.Containers = append(containers, container)

		return injectedDeployment
	}

	// GIVEN
	dummyDeployment1 := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-deploy-1",
		},
		Spec: appsv1.DeploymentSpec{
			Template: v1.PodTemplateSpec{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "nginx",
							Image: "nginx",
						},
					},
				},
			},
		},
	}
	dummyDeployment2 := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-deploy-2",
		},
		Spec: appsv1.DeploymentSpec{
			Template: v1.PodTemplateSpec{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "echo-server",
							Image: "ghcr.io/wzshiming/echoserver/echoserver:v0.0.1",
						},
					},
				},
			},
		},
	}

	sidecar := v1.Container{Name: "sidecar", Image: "fake-image"}
	expectedDeployment1 := appendContainer(dummyDeployment1, sidecar)
	expectedDeployment2 := appendContainer(dummyDeployment2, sidecar)

	injector := injectorImpl{
		objects: []*unstructured.Unstructured{
			toUnstructured(dummyDeployment1),
			toUnstructured(dummyDeployment2),
		},
	}

	expected := []*unstructured.Unstructured{
		toUnstructured(expectedDeployment1),
		toUnstructured(expectedDeployment2),
	}

	// WHEN
	actual, err := injector.Inject(sidecar)

	// THEN
	if assert.NoError(t, err) {
		assert.Equal(t, expected, actual)
	}
}
