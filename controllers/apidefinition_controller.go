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

package controllers

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/TykTechnologies/tyk-operator/pkg/cert"
	"github.com/TykTechnologies/tyk-operator/pkg/client/klient"
	"github.com/TykTechnologies/tyk-operator/pkg/environmet"
	"github.com/TykTechnologies/tyk-operator/pkg/keys"

	"github.com/TykTechnologies/tyk-operator/api/model"
	tykv1alpha1 "github.com/TykTechnologies/tyk-operator/api/v1alpha1"
	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	util "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const queueAfter = time.Second * 5

// ApiDefinitionReconciler reconciles a ApiDefinition object
type ApiDefinitionReconciler struct {
	client.Client
	Log      logr.Logger
	Scheme   *runtime.Scheme
	Env      environmet.Env
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=tyk.tyk.io,resources=apidefinitions,verbs=get;list;watch;create;update;patch;delete;deletecollection
// +kubebuilder:rbac:groups=tyk.tyk.io,resources=apidefinitions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;update;create
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;create;update

func (r *ApiDefinitionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	namespacedName := req.NamespacedName
	log := r.Log.WithValues("ApiDefinition", namespacedName.String())

	log.Info("Reconciling ApiDefinition instance")

	desired := &tykv1alpha1.ApiDefinition{}

	if err := r.Get(ctx, req.NamespacedName, desired); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err) // Ignore not-found errors
	}

	upstreamRequestStruct := &tykv1alpha1.ApiDefinition{}
	desired.DeepCopyInto(upstreamRequestStruct)

	// set context for all api calls inside this reconciliation loop
	env, ctx, err := httpContext(ctx, r.Client, r.Env, desired, log)
	if err != nil {
		return ctrl.Result{}, err
	}

	if desired.GetLabels()["template"] == "true" {
		log.Info("Syncing template", "template", desired.Name)

		res, err := r.syncTemplate(ctx, req.Namespace, desired)
		if err != nil {
			log.Error(err, "Failed to sync template")
			return res, err
		}

		log.Info("Synced template", "template", desired.Name)

		return ctrl.Result{}, nil
	}

	var queueA time.Duration

	_, err = util.CreateOrUpdate(ctx, r.Client, desired, func() error {
		if !desired.ObjectMeta.DeletionTimestamp.IsZero() {
			e, err := r.delete(ctx, desired)
			queueA = e
			return err
		}

		if desired.Spec.APIID == "" {
			upstreamRequestStruct.Spec.APIID = encodeNS(req.NamespacedName.String())
		}

		if desired.Spec.OrgID == "" {
			upstreamRequestStruct.Spec.OrgID = env.Org
		}

		util.AddFinalizer(desired, keys.ApiDefFinalizerName)

		if len(desired.Spec.UpstreamCertificateRefs) != 0 {
			for domain, certName := range desired.Spec.UpstreamCertificateRefs {
				tykCertID, err := r.checkSecretAndUpload(ctx, certName, namespacedName, log, &env)
				if err != nil {
					// we should log the missing secret but we should still create the API definition
					log.Info(fmt.Sprintf("cert name %s is missing", certName))
				} else {
					if upstreamRequestStruct.Spec.UpstreamCertificates == nil {
						upstreamRequestStruct.Spec.UpstreamCertificates = make(map[string]string)
					}
					upstreamRequestStruct.Spec.UpstreamCertificates[domain] = tykCertID
				}
			}
		}
		upstreamRequestStruct.Spec.UpstreamCertificateRefs = nil

		// we support only one certificate secret name for mvp
		if len(desired.Spec.CertificateSecretNames) != 0 {
			certName := desired.Spec.CertificateSecretNames[0]
			tykCertID, err := r.checkSecretAndUpload(ctx, certName, namespacedName, log, &env)
			if err != nil {
				return err
			}

			upstreamRequestStruct.Spec.Certificates = []string{tykCertID}
		}

		upstreamRequestStruct.Spec.CertificateSecretNames = nil
		r.updateLinkedPolicies(ctx, upstreamRequestStruct)

		targets := desired.Spec.CollectLoopingTarget()

		if err := r.ensureTargets(ctx, desired.Namespace, targets); err != nil {
			return err
		}
		err := r.updateLoopingTargets(ctx, desired, targets)
		if err != nil {
			log.Error(err, "Failed to update looping targets")
			return err
		}

		desired.Spec.CollectLoopingTarget()
		//  If this is not set, means it is a new object, set it first
		if desired.Status.ApiID == "" {
			return r.create(ctx, upstreamRequestStruct, log)
		}

		return r.update(ctx, upstreamRequestStruct, log)
	})

	if err == nil {
		log.Info("Completed reconciling ApiDefinition instance")
	} else {
		queueA = queueAfter
	}

	return ctrl.Result{RequeueAfter: queueA}, err
}

func (r *ApiDefinitionReconciler) checkSecretAndUpload(
	ctx context.Context,
	certName string,
	namespacedName types.NamespacedName,
	log logr.Logger,
	env *environmet.Env,
) (string, error) {
	secret := v1.Secret{}

	err := r.Get(ctx, types.NamespacedName{Name: certName, Namespace: namespacedName.Namespace}, &secret)
	if err != nil {
		log.Error(err, "requeueing because secret not found")
		return "", err
	}

	pemCrtBytes, ok := secret.Data["tls.crt"]
	if !ok {
		err = fmt.Errorf("%s", "requeueing because cert not found in secret")
		log.Error(err, "requeueing because cert not found in secret")

		return "", err
	}

	pemKeyBytes, ok := secret.Data["tls.key"]
	if !ok {
		err = fmt.Errorf("%s", "requeueing because key not found in secret")
		log.Error(err, "requeueing because key not found in secret")

		return "", err
	}

	tykCertID := env.Org + cert.CalculateFingerPrint(pemCrtBytes)
	exists := klient.Universal.Certificate().Exists(ctx, tykCertID)

	if !exists {
		// upload the certificate
		tykCertID, err = klient.Universal.Certificate().Upload(ctx, pemKeyBytes, pemCrtBytes)
		if err != nil {
			return "", err
		}
	}

	return tykCertID, nil
}

func (r *ApiDefinitionReconciler) create(
	ctx context.Context,
	desired *tykv1alpha1.ApiDefinition,
	log logr.Logger,
) error {
	log.Info("Creating new  ApiDefinition")

	_, err := klient.Universal.Api().Create(ctx, &desired.Spec.APIDefinitionSpec)
	if err != nil {
		log.Error(err, "Failed to create api definition")
		return err
	}

	desired.Status.ApiID = desired.Spec.APIID

	err = r.Status().Update(ctx, desired)
	if err != nil {
		log.Error(err, "Could not update Status ID")
	}

	klient.Universal.HotReload(ctx)

	return nil
}

func (r *ApiDefinitionReconciler) update(
	ctx context.Context,
	desired *tykv1alpha1.ApiDefinition,
	log logr.Logger,
) error {
	log.Info("Updating ApiDefinition")

	_, err := klient.Universal.Api().Update(ctx, &desired.Spec.APIDefinitionSpec)
	if err != nil {
		log.Error(err, "Failed to update api definition")
		return err
	}

	klient.Universal.HotReload(ctx)

	return nil
}

// This triggers an update to all ingress resources that have template
// annotation matching a.Name.
//
// We return nil when a is being deleted and do nothing.
func (r *ApiDefinitionReconciler) syncTemplate(ctx context.Context, ns string, a *tykv1alpha1.ApiDefinition) (ctrl.Result, error) {
	if !a.DeletionTimestamp.IsZero() {
		if util.ContainsFinalizer(a, keys.ApiDefTemplateFinalizerName) {
			ls := netv1.IngressList{}

			err := r.List(ctx, &ls,
				client.InNamespace(ns),
			)
			if err != nil {
				if !errors.IsNotFound(err) {
					return ctrl.Result{}, err
				}
			}

			var refs []string

			for _, v := range ls.Items {
				if v.GetAnnotations()[keys.IngressTemplateAnnotation] == a.Name {
					refs = append(refs, v.GetName())
				}
			}

			if len(refs) > 0 {
				return ctrl.Result{RequeueAfter: time.Second * 5}, fmt.Errorf("Can't delete %s %v depends on it", a.Name, refs)
			}

			util.RemoveFinalizer(a, keys.ApiDefTemplateFinalizerName)

			return ctrl.Result{}, r.Update(ctx, a)
		}

		return ctrl.Result{}, nil
	}

	if !util.ContainsFinalizer(a, keys.ApiDefTemplateFinalizerName) {
		util.AddFinalizer(a, keys.ApiDefTemplateFinalizerName)

		return ctrl.Result{}, r.Update(ctx, a)
	}

	ls := netv1.IngressList{}

	err := r.List(ctx, &ls,
		client.InNamespace(ns),
	)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	for _, v := range ls.Items {
		if v.GetAnnotations()[keys.IngressTemplateAnnotation] == a.Name {
			key := client.ObjectKey{
				Namespace: v.GetNamespace(),
				Name:      v.GetName(),
			}

			r.Log.Info("Updating ingress " + key.String())

			if v.Labels == nil {
				v.Labels = make(map[string]string)
			}

			v.Labels[keys.IngressTaintLabel] = strconv.FormatInt(time.Now().UnixNano(), 10)

			err = r.Update(ctx, &v)
			if err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{}, nil
}

func (r *ApiDefinitionReconciler) delete(ctx context.Context, desired *tykv1alpha1.ApiDefinition) (time.Duration, error) {
	r.Log.Info("resource being deleted")
	// If our finalizer is present, need to delete from Tyk still
	if util.ContainsFinalizer(desired, keys.ApiDefFinalizerName) {
		if err := r.checkLinkedPolicies(ctx, desired); err != nil {
			return queueAfter, err
		}

		if err := r.checkLoopingTargets(ctx, desired); err != nil {
			return queueAfter, err
		}

		ns := model.Target{
			Name:      desired.Name,
			Namespace: desired.Namespace,
		}

		for _, target := range desired.Status.LinkedToAPIs {
			err := r.updateStatus(ctx, desired.Namespace, target, true, func(ads *tykv1alpha1.ApiDefinitionStatus) {
				ads.LinkedByAPIs = removeTarget(ads.LinkedByAPIs, ns)
			})
			if err != nil {
				return queueAfter, err
			}
		}

		r.Log.Info("deleting api")

		_, err := klient.Universal.Api().Delete(ctx, desired.Status.ApiID)
		if err != nil {
			r.Log.Error(err, "unable to delete api", "api_id", desired.Status.ApiID)
			return 0, err
		}

		err = klient.Universal.HotReload(ctx)
		if err != nil {
			r.Log.Error(err, "unable to hot reload", "api_id", desired.Status.ApiID)
			return 0, err
		}

		r.Log.Info("removing finalizer")
		util.RemoveFinalizer(desired, keys.ApiDefFinalizerName)
	}

	return 0, nil
}

// checkLinkedPolicies checks if there are any policies that are still linking
// to this api defintion resource.
func (r *ApiDefinitionReconciler) checkLinkedPolicies(ctx context.Context, a *tykv1alpha1.ApiDefinition) error {
	r.Log.Info("checking linked security policies")

	if len(a.Status.LinkedByPolicies) == 0 {
		return nil
	}

	for _, n := range a.Status.LinkedByPolicies {
		var api tykv1alpha1.SecurityPolicy

		if err := r.Get(ctx, n.NS(a.Namespace), &api); err == nil {
			return fmt.Errorf("unable to delete api due to security policy dependency=%s", n)
		}
	}

	return nil
}

func encodeIfNotBase64(s string) string {
	_, err := base64.RawURLEncoding.DecodeString(s)
	if err == nil {
		return s
	}

	return encodeNS(s)
}

// updateLinkedPolicies ensure that all policies needed by this api denition are
// updated.
func (r *ApiDefinitionReconciler) updateLinkedPolicies(ctx context.Context, a *tykv1alpha1.ApiDefinition) {
	r.Log.Info("Updating linked policies")

	for x := range a.Spec.JWTDefaultPolicies {
		a.Spec.JWTDefaultPolicies[x] = encodeIfNotBase64(a.Spec.JWTDefaultPolicies[x])
	}

	for k, x := range a.Spec.JWTScopeToPolicyMapping {
		a.Spec.JWTScopeToPolicyMapping[k] = encodeIfNotBase64(x)
	}
}

// checkLoopingTargets Check if there is any other api's linking to a
func (r *ApiDefinitionReconciler) checkLoopingTargets(ctx context.Context, a *tykv1alpha1.ApiDefinition) error {
	r.Log.Info("checking linked api resources")

	if len(a.Status.LinkedByAPIs) == 0 {
		return nil
	}

	for _, n := range a.Status.LinkedByAPIs {
		var api tykv1alpha1.ApiDefinition

		if err := r.Get(ctx, n.NS(a.Namespace), &api); err == nil {
			return fmt.Errorf("unable to delete api due to being depended by =%s", n)
		}
	}

	return nil
}

func (r *ApiDefinitionReconciler) ensureTargets(
	ctx context.Context,
	ns string,
	targets []model.Target,
) error {
	for _, target := range targets {
		var api tykv1alpha1.ApiDefinition

		if err := r.Get(ctx, target.NS(ns), &api); err != nil {
			return err
		}
	}

	return nil
}

func (r *ApiDefinitionReconciler) updateLoopingTargets(ctx context.Context,
	a *tykv1alpha1.ApiDefinition, links []model.Target,
) error {
	r.Log.Info("updating looping targets")

	if len(links) == 0 {
		return nil
	}

	ns := model.Target{
		Name:      a.Name,
		Namespace: a.Namespace,
	}

	for _, target := range links {
		err := r.updateStatus(ctx, a.Namespace, target, false, func(ads *tykv1alpha1.ApiDefinitionStatus) {
			ads.LinkedByAPIs = addTarget(ads.LinkedByAPIs, ns)
			sort.Slice(ads.LinkedByAPIs, func(i, j int) bool {
				return ads.LinkedByAPIs[i].String() < ads.LinkedByAPIs[j].String()
			})
		})
		if err != nil {
			return err
		}
	}

	// we need to update removed targets
	newTargets := make(map[string]model.Target)
	for _, v := range links {
		newTargets[v.String()] = v
	}

	for _, v := range a.Status.LinkedToAPIs {
		if _, ok := newTargets[v.String()]; !ok {
			err := r.updateStatus(ctx, a.Namespace, v, true, func(ads *tykv1alpha1.ApiDefinitionStatus) {
				ads.LinkedByAPIs = removeTarget(ads.LinkedByAPIs, ns)
				sort.Slice(ads.LinkedByAPIs, func(i, j int) bool {
					return ads.LinkedByAPIs[i].String() < ads.LinkedByAPIs[j].String()
				})
			})
			if err != nil {
				return err
			}
		}
	}

	a.Status.LinkedToAPIs = links

	return client.IgnoreNotFound(r.Status().Update(ctx, a))
}

func (r *ApiDefinitionReconciler) updateStatus(
	ctx context.Context,
	ns string,
	target model.Target,
	ignoreNotFound bool,
	fn func(*tykv1alpha1.ApiDefinitionStatus),
) error {
	var api tykv1alpha1.ApiDefinition
	if err := r.Get(ctx, target.NS(ns), &api); err != nil {
		if errors.IsNotFound(err) {
			if ignoreNotFound {
				return nil
			}
		}

		return fmt.Errorf("unable to get api %v %v", target.NS(ns), err)
	}

	fn(&api.Status)

	return r.Status().Update(ctx, &api)
}

// SetupWithManager initializes the api definition controller.
func (r *ApiDefinitionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tykv1alpha1.ApiDefinition{}).
		Owns(&v1.Secret{}).
		Complete(r)
}
