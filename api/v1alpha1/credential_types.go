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

type SecurityPolicyNamespacedName struct {
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

// CredentialSpec defines the desired state of Credential
type CredentialSpec struct {

	// LastCheck Deprecated
	//LastCheck                     int64                       `json:"last_check" msg:"last_check"`
	// Allowance Deprecated
	//Allowance                     float64                     `json:"allowance" msg:"allowance"`

	// Rate represents the number of requests allowed in the specified rate limiting window
	Rate int64 `json:"rate" msg:"rate,omitempty"`

	// Per represents the size of the rate limiting window in seconds
	Per int64 `json:"per" msg:"per,omitempty"`

	// Expires represents when a credential should expire. 0 disables credential expiry.
	Expires int64 `json:"expires" msg:"expires,omitempty"`

	// QuotaRenews is an epoch that defines when the quota renews
	QuotaRenews int `json:"quota_renews,omitempty"`

	// QuotaRenewalRate represents the size of the quota window in seconds. For 1 hour, this value would be 3600.
	QuotaRenewalRate int `json:"quota_renewal_rate,omitempty"`

	// QuotaMax defines the maximum number of requests permitted within QuotaRenewalRate seconds
	QuotaMax int `json:"quota_max,omitempty"`

	// AccessRights Defines which APIs and versions this credential has access to
	AccessRights map[string]AccessRight `json:"access_rights,omitempty"`

	// ApplyPolicies specifies a list of policy_ids to apply to this credential.
	// Do not use directly. Prefer using ApplyPolicyRefs instead which allow you to specify a namespacedName instead.
	ApplyPolicies []string `json:"apply_policies,omitempty"`

	// ApplyPolicyRefs specifies a list of SecurityPolicies to apply to this credential.
	ApplyPolicyRefs []SecurityPolicyNamespacedName `json:"apply_policy_refs,omitempty"`

	// OrgID represents the tyk organization this credential belongs to. It can be used in conjunction. This is automatically
	// set by Tyk Operator
	OrgID string `json:"org_id,omitempty"`
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
