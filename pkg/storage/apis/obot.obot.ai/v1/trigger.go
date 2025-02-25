package v1

import (
	"slices"

	"github.com/obot-platform/nah/pkg/fields"
	"github.com/obot-platform/obot/apiclient/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	_ fields.Fields = (*Trigger)(nil)
	_ Generationed  = (*Trigger)(nil)
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type TriggerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []Trigger `json:"items"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type Trigger struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TriggerSpec   `json:"spec"`
	Status TriggerStatus `json:"status,omitempty"`
}

type TriggerSpec struct {
	types.TriggerManifest
	Workflow   string `json:"workflow"`
	ThreadName string
}

type TriggerStatus struct {
	OptionsValid       *bool `json:"optionsValid,omitempty"`
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

func (d *Trigger) Has(field string) (exists bool) {
	return slices.Contains(d.FieldNames(), field)
}

func (d *Trigger) Get(field string) (value string) {
	switch field {
	case "spec.threadName":
		return d.Spec.ThreadName
	case "spec.workflow":
		return d.Spec.Workflow
	case "spec.provider":
		return d.Spec.Provider
	}
	return ""
}

func (d *Trigger) FieldNames() []string {
	return []string{"spec.threadName", "spec.workflow", "spec.provider"}
}

func (*Trigger) GetColumns() [][]string {
	return [][]string{
		{"Name", "Name"},
		{"Workflow", "Spec.Workflow"},
		{"Trigger Provider", "Spec.Provider"},
		{"Configuration Valid", "Status.OptionsValid"},
		{"Created", "{{ago .CreationTimestamp}}"},
		{"Description", "Spec.Description"},
	}
}

func (d *Trigger) GetObservedGeneration() int64 {
	return d.Status.ObservedGeneration
}

func (d *Trigger) SetObservedGeneration(gen int64) {
	d.Status.ObservedGeneration = gen
}

func (*Trigger) DeleteRefs() []Ref {
	return nil
}
