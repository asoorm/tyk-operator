/*


Licensed under the Mozilla Public License (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.mozilla.org/en-US/MPL/2.0/

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type AccessRight struct {
	ApiName  string   `json:"api_name,omitempty"`
	ApiID    string   `json:"api_id,omitempty"`
	Versions []string `json:"versions,omitempty"`
}

// CredentialSpec defines the desired state of Credential
type CredentialSpec struct {
	Rate             int                    `json:"rate,omitempty"`
	Per              int                    `json:"per,omitempty"`
	OrgID            string                 `json:"org_id,omitempty"`
	Allowance        int                    `json:"allowance,omitempty"`
	QuotaRenews      int                    `json:"quota_renews,omitempty"`
	QuotaRenewalRate int                    `json:"quota_renewal_rate,omitempty"`
	QuotaMax         int                    `json:"quota_max,omitempty"`
	AccessRights     map[string]AccessRight `json:"access_rights,omitempty"`
}

// CredentialStatus defines the observed state of Credential
type CredentialStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Credential is the Schema for the credentials API
type Credential struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CredentialSpec   `json:"spec,omitempty"`
	Status CredentialStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// CredentialList contains a list of Credential
type CredentialList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Credential `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Credential{}, &CredentialList{})
}
