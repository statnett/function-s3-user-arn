package main

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/crossplane/function-sdk-go/logging"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/resource"
	"github.com/crossplane/function-sdk-go/response"
)

func TestRunFunction(t *testing.T) {

	type args struct {
		ctx context.Context
		req *fnv1.RunFunctionRequest
	}
	type want struct {
		rsp *fnv1.RunFunctionResponse
		err error
	}

	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"ExternalResourcesAreRequired": {
			reason: "The Function requires more external resources",
			args: args{
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "s3-user-arn.fn.crossplane.io/v1alpha1",
						"kind": "Input"
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "s3.statnett.no/v1alpha1",
								"kind": "Bucket",
								"metadata": {
									"name": "test"
								},
								"spec": {
									"accountRef": {
										"name": "account"
									},
									"permissions": [
										{
											"principals": [
												{
													"user": "test"
												}
											]
										}
									]
								}
							}`),
						},
					},
					Context: resource.MustStructJSON(`{
						"apiextensions.crossplane.io/environment": {
							"tenantName": "tenant"
						}
					}`),
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Context: resource.MustStructJSON(`{
						"apiextensions.crossplane.io/environment": {
							"tenantName": "tenant"
						}
					}`),
					Requirements: &fnv1.Requirements{
						ExtraResources: map[string]*fnv1.ResourceSelector{
							"tenant account test": {
								ApiVersion: "iam.aws.upbound.io/v1beta1",
								Kind:       "User",
								Match: &fnv1.ResourceSelector_MatchLabels{
									MatchLabels: &fnv1.MatchLabels{
										Labels: map[string]string{
											"s3.statnett.no/tenant-name":  "tenant",
											"s3.statnett.no/account-name": "account",
											"crossplane.io/claim-name":    "test",
										},
									},
								},
							},
						},
					},
				},
			},
		},
		"ResponseIsReturned": {
			reason: "The Function should return a successful result if sufficient resources are provided",
			args: args{
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "s3-user-arn.fn.crossplane.io/v1alpha1",
						"kind": "Input"
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
								"apiVersion": "s3.statnett.no/v1alpha1",
								"kind": "Bucket",
								"metadata": {
									"name": "test"
								},
								"spec": {
									"accountRef": {
										"name": "account"
									},
									"permissions": [
										{
											"principals": [
												{
													"user": "test"
												}
											]
										}
									]
								}
							}`),
						},
					},
					Context: resource.MustStructJSON(`{
						"apiextensions.crossplane.io/environment": {
							"tenantName": "tenant"
						}
					}`),
					ExtraResources: map[string]*fnv1.Resources{
						"tenant account test": {
							Items: []*fnv1.Resource{
								{
									Resource: resource.MustStructJSON(`{
										"apiVersion": "iam.aws.upbound.io/v1beta1",
										"kind": "User",
										"metadata": {
											"name": "test",
											"namespace": "test"
										},
										"status": {
											"forProvider": {
												"arn": "test"
											}
										}
									}`),
								},
							},
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Context: resource.MustStructJSON(`{
						"apiextensions.crossplane.io/environment": {
							"tenantName": "tenant"
						},
						"s3-user-arn.fn.crossplane.io": {
							"tenant account test": [
								{
									"apiVersion": "iam.aws.upbound.io/v1beta1",
									"kind": "User",
									"metadata": {
										"name": "test",
										"namespace": "test"
									},
									"status": {
										"forProvider": {
											"arn": "test"
										}
									}
								}
							]
						}
					}`),
					Requirements: &fnv1.Requirements{
						ExtraResources: map[string]*fnv1.ResourceSelector{
							"tenant account test": {
								ApiVersion: "iam.aws.upbound.io/v1beta1",
								Kind:       "User",
								Match: &fnv1.ResourceSelector_MatchLabels{
									MatchLabels: &fnv1.MatchLabels{
										Labels: map[string]string{
											"s3.statnett.no/tenant-name":  "tenant",
											"s3.statnett.no/account-name": "account",
											"crossplane.io/claim-name":    "test",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := &Function{log: logging.NewNopLogger()}
			rsp, err := f.RunFunction(tc.args.ctx, tc.args.req)

			if diff := cmp.Diff(tc.want.rsp, rsp, protocmp.Transform()); diff != "" {
				t.Errorf("%s\nf.RunFunction(...): -want rsp, +got rsp:\n%s", tc.reason, diff)
			}

			if diff := cmp.Diff(tc.want.err, err, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("%s\nf.RunFunction(...): -want err, +got err:\n%s", tc.reason, diff)
			}
		})
	}
}
