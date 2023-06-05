package report

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	errorMachinery "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	polReport "sigs.k8s.io/wg-policy-prototypes/policy-report/pkg/api/wgpolicyk8s.io/v1alpha2"
)

// PolicyReportStore caches the latest version of PolicyReports
type PolicyReportStore struct {
	// namespacedPolicyReports is a map of namespaces and namespaced PolicyReports
	namespacedPolicyReports map[string]PolicyReport
	// clusterPolicyReport is the sole ClusterPolicyReport
	clusterPolicyReport ClusterPolicyReport

	namespacedPolicyReportsMutex *sync.RWMutex
	clusterPolicyReportMutex     *sync.RWMutex

	// client used to instantiate PolicyReport resources
	client client.Client
}

// NewPolicyReportStore construct a PolicyReportStore, initializing the
// clusterwide ClusterPolicyReport and namesapcedPolicyReports.
func NewPolicyReportStore() (*PolicyReportStore, error) {
	config := ctrl.GetConfigOrDie()
	customScheme := scheme.Scheme
	customScheme.AddKnownTypes(
		polReport.SchemeGroupVersion,
		&polReport.PolicyReport{},
		&polReport.PolicyReportList{},
		&polReport.ClusterPolicyReportList{},
	)
	metav1.AddToGroupVersion(customScheme, polReport.SchemeGroupVersion)
	client, err := client.New(config, client.Options{Scheme: customScheme})
	if err != nil {
		return nil, fmt.Errorf("failed when creating new client: %w", err)
	}

	return &PolicyReportStore{
		namespacedPolicyReports:      make(map[string]PolicyReport),
		clusterPolicyReport:          NewClusterPolicyReport("clusterwide"),
		namespacedPolicyReportsMutex: new(sync.RWMutex),
		clusterPolicyReportMutex:     new(sync.RWMutex),
		client:                       client,
	}, nil
}

// MockNewPolicyReportStore constructs a PolicyReportStore, initializing the
// clusterwide ClusterPolicyReport and namespacedPolicyReports, but setting the
// client to nil. Useful for testing.
func MockNewPolicyReportStore() *PolicyReportStore {
	return &PolicyReportStore{
		namespacedPolicyReports:      make(map[string]PolicyReport),
		clusterPolicyReport:          NewClusterPolicyReport("clusterwide"),
		namespacedPolicyReportsMutex: new(sync.RWMutex),
		clusterPolicyReportMutex:     new(sync.RWMutex),
		client:                       nil,
	}
}

// AddPolicyReport adds a namespaced PolicyReport to the Store
func (s *PolicyReportStore) AddPolicyReport(report *PolicyReport) error {
	s.namespacedPolicyReportsMutex.Lock()
	defer s.namespacedPolicyReportsMutex.Unlock()
	s.namespacedPolicyReports[report.GetNamespace()] = *report
	return nil
}

// AddClusterPolicyReport adds the ClusterPolicyReport to the Store
func (s *PolicyReportStore) AddClusterPolicyReport(report *ClusterPolicyReport) error {
	s.clusterPolicyReportMutex.Lock()
	defer s.clusterPolicyReportMutex.Unlock()
	s.clusterPolicyReport = *report
	return nil
}

// Get PolicyReport by namespace
func (s *PolicyReportStore) GetPolicyReport(namespace string) (PolicyReport, error) {
	s.namespacedPolicyReportsMutex.RLock()
	defer s.namespacedPolicyReportsMutex.RUnlock()
	report, present := s.namespacedPolicyReports[namespace]
	if present {
		return report, nil
	}
	return PolicyReport{}, errors.New("report not found")
}

// Get the ClusterPolicyReport
func (s *PolicyReportStore) GetClusterPolicyReport() (ClusterPolicyReport, error) {
	s.clusterPolicyReportMutex.RLock()
	defer s.clusterPolicyReportMutex.RUnlock()
	report := s.clusterPolicyReport
	return report, nil
}

// Update namespaced PolicyReport
func (s *PolicyReportStore) UpdatePolicyReport(report *PolicyReport) error {
	s.namespacedPolicyReportsMutex.Lock()
	defer s.namespacedPolicyReportsMutex.Unlock()
	s.namespacedPolicyReports[report.GetNamespace()] = *report
	return nil
}

// Update ClusterPolicyReport or PolicyReport. ns argument is used in case
// of namespaced PolicyReport
func (s *PolicyReportStore) UpdateClusterPolicyReport(report *ClusterPolicyReport) error {
	s.clusterPolicyReportMutex.Lock()
	defer s.clusterPolicyReportMutex.Unlock()
	s.clusterPolicyReport = *report
	return nil
}

// Delete PolicyReport by namespace
func (s *PolicyReportStore) RemovePolicyReport(namespace string) error {
	if _, err := s.GetPolicyReport(namespace); err == nil {
		s.namespacedPolicyReportsMutex.Lock()
		defer s.namespacedPolicyReportsMutex.Unlock()
		delete(s.namespacedPolicyReports, namespace)
	}
	return nil
}

// Delete all namespaced PolicyReports
func (s *PolicyReportStore) RemoveAllNamespacedPolicyReports() error {
	s.namespacedPolicyReportsMutex.Lock()
	defer s.namespacedPolicyReportsMutex.Unlock()
	// TODO once go 1.21 is out, use new `clear` builtin
	s.namespacedPolicyReports = make(map[string]PolicyReport)
	return nil
}

// Marshal the contents of the store into a JSON string
func (s *PolicyReportStore) ToJSON() (string, error) {
	recapJSON := make(map[string]interface{})
	recapJSON["cluster"] = s.clusterPolicyReport
	recapJSON["namespaces"] = s.namespacedPolicyReports

	marshaled, err := json.Marshal(recapJSON)
	if err != nil {
		return "", err
	}
	return string(marshaled), nil
}

// Save instantiates the passed namespaced PolicyReport if it doesn't exist, or
// updated a new one if one is found
func (s *PolicyReportStore) Save(report *PolicyReport) error {
	// Check for existing Policy Report
	result := &polReport.PolicyReport{}
	getErr := s.client.Get(context.TODO(), types.NamespacedName{
		Namespace: report.Namespace,
		Name:      report.Name,
	}, result)
	// Create new Policy Report if not found
	if errorMachinery.IsNotFound(getErr) {
		log.Info().Msg("creating policy report...")
		err := s.client.Create(context.TODO(), report)
		if err != nil {
			return fmt.Errorf("failed when creating PolicyReport: %w", err)
		}
	} else {
		// Update existing Policy Report
		log.Info().Msg("updating policy report...")
		retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			getObj := &polReport.PolicyReport{}
			err := s.client.Get(context.TODO(), types.NamespacedName{
				Namespace: report.Namespace,
				Name:      report.Name,
			}, getObj)
			if errorMachinery.IsNotFound(err) {
				// This should never happen
				log.Error().Err(err).Str("PolicyReport name", report.GetName())
				return nil
			}
			if err != nil {
				return fmt.Errorf("failed when getting PolicyReport: %w", err)
			}
			report.SetResourceVersion(getObj.GetResourceVersion())
			updateErr := s.client.Update(context.TODO(), report)
			// return unwrapped error for RetryOnConflict()
			return updateErr
		})
		if retryErr != nil {
			log.Error().
				Dict("dict", zerolog.Dict().
					Str("report name", report.Name).Str("report ns", report.Namespace),
				).Msg("PolicyReport update failed")
		}
		log.Info().
			Dict("dict", zerolog.Dict().
				Str("report name", report.Name).Str("report ns", report.Namespace),
			).Msg("updated PolicyReport")
	}
	return nil
}
