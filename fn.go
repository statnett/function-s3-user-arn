package main

import (
	"context"
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/crossplane/function-sdk-go/errors"
	"github.com/crossplane/function-sdk-go/logging"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/request"
	"github.com/crossplane/function-sdk-go/resource"
	"github.com/crossplane/function-sdk-go/response"
	"github.com/crossplane/user-s3-arn/input/v1alpha1"
)

// Key to retrieve extras at.
const (
	FunctionContextKeyS3UserARN = "s3-user-arn.fn.crossplane.io"
)

// Function returns whatever response you ask it to.
type Function struct {
	fnv1.UnimplementedFunctionRunnerServiceServer

	log logging.Logger
}

// RunFunction runs the Function.
func (f *Function) RunFunction(_ context.Context, req *fnv1.RunFunctionRequest) (*fnv1.RunFunctionResponse, error) {
	f.log.Info("Running function", "tag", req.GetMeta().GetTag())

	rsp := response.To(req, response.DefaultTTL)

	in := &v1alpha1.Input{}
	if err := request.GetInput(req, in); err != nil {
		// You can set a custom status condition on the claim. This allows you to
		// communicate with the user. See the link below for status condition
		// guidance.
		// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties
		response.ConditionFalse(rsp, "FunctionSuccess", "InternalError").
			WithMessage("Something went wrong.").
			TargetCompositeAndClaim()

		// You can emit an event regarding the claim. This allows you to communicate
		// with the user. Note that events should be used sparingly and are subject
		// to throttling; see the issue below for more information.
		// https://github.com/crossplane/crossplane/issues/5802
		response.Warning(rsp, errors.New("something went wrong")).
			TargetCompositeAndClaim()

		response.Fatal(rsp, errors.Wrapf(err, "cannot get Function input from %T", req))
		return rsp, nil
	}

	// Get XR the pipeline targets.
	oxr, err := request.GetObservedCompositeResource(req)
	if err != nil {
		response.Fatal(rsp, errors.Errorf("cannot get observed composite resource: %w", err))
		return rsp, nil
	}

	// Build extraResource Requests.
	rsp.Requirements = buildRequirements(in, oxr, req.GetContext())

	// The request response cycle for the Crossplane ExtraResources API requires that function-extra-resources
	// tells Crossplane what it wants.
	// Then a new rquest is sent to function-extra-resources with those resources present at the ExtraResources field.
	//
	// function-extra-resources does not know if it has requested the resources already or not.
	//
	// If it has and these resources are now present, proceed with verification and conversion.
	if len(rsp.GetRequirements().GetExtraResources()) > 0 && req.ExtraResources == nil {
		f.log.Debug("No extra resources present, exiting", "requirements", rsp.GetRequirements())
		return rsp, nil
	}

	// Pull extra resources from the ExtraResources request field.
	extraResources, err := request.GetExtraResources(req)
	if err != nil {
		response.Fatal(rsp, errors.Errorf("fetching extra resources %T: %w", req, err))
		return rsp, nil
	}

	cleanedExtras := make(map[string][]unstructured.Unstructured, len(extraResources))
	for k, r := range extraResources {
		tmpExtra := make([]unstructured.Unstructured, 0, len(r))
		for _, extra := range r {
			tmpExtra = append(tmpExtra, *extra.Resource)
		}
		cleanedExtras[k] = tmpExtra
	}

	b, err := json.Marshal(cleanedExtras)
	if err != nil {
		response.Fatal(rsp, errors.Errorf("cannot marshal %T: %w", cleanedExtras, err))
		return rsp, nil
	}
	s := &structpb.Struct{}
	err = protojson.Unmarshal(b, s)
	if err != nil {
		response.Fatal(rsp, errors.Errorf("cannot unmarshal %T into %T: %w", b, s, err))
		return rsp, nil
	}
	response.SetContextKey(rsp, FunctionContextKeyS3UserARN, structpb.NewStructValue(s))

	return rsp, nil
}

// Build requirements takes input and outputs an array of external resoruce requirements to request
// from Crossplane's external resource API.
func buildRequirements(_ *v1alpha1.Input, xr *resource.Composite, context *structpb.Struct) *fnv1.Requirements {
	spec := xr.Resource.Object["spec"].(map[string]any)

	env := context.GetFields()["apiextensions.crossplane.io/environment"].GetStructValue()
	if env == nil {
		return &fnv1.Requirements{}
	}

	observedTenant := env.GetFields()["tenantName"].GetStringValue()
	observedAccount := spec["accountRef"].(map[string]any)["name"].(string)

	extraResources := make(map[string]*fnv1.ResourceSelector)
	permissions, ok := spec["permissions"].([]any)
	if ok {
		for _, permission := range permissions {
			for _, principal := range permission.(map[string]any)["principals"].([]any) {
				principal := principal.(map[string]any)
				if user, ok := principal["user"]; ok {
					user := user.(string)
					tenant := observedTenant
					if t, ok := principal["tenant"]; ok {
						tenant = t.(string)
					}
					account := observedAccount
					if a, ok := principal["account"]; ok {
						account = a.(string)
					}

					key := fmt.Sprintf("%s %s %s", tenant, account, user)
					extraResources[key] = &fnv1.ResourceSelector{
						ApiVersion: "iam.aws.upbound.io/v1beta1",
						Kind:       "User",
						Match: &fnv1.ResourceSelector_MatchLabels{
							MatchLabels: &fnv1.MatchLabels{
								Labels: map[string]string{
									"s3.statnett.no/tenant-name":  tenant,
									"s3.statnett.no/account-name": account,
									"crossplane.io/claim-name":    user,
								},
							},
						},
					}
				}
			}
		}
	}
	return &fnv1.Requirements{ExtraResources: extraResources}
}
