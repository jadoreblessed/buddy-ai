package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type HTTPOnboardingSource struct {
	baseURL string
	client  *http.Client
}

func NewHTTPOnboardingSource(baseURL string) *HTTPOnboardingSource {
	return &HTTPOnboardingSource{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 5 * time.Second},
	}
}

func (s *HTTPOnboardingSource) GetPlan(ctx context.Context, internID string) (AssignedPlan, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		s.baseURL+"/interns/"+url.PathEscape(internID)+"/onboarding-plan", nil)
	if err != nil {
		return AssignedPlan{}, err
	}
	response, err := s.client.Do(request)
	if err != nil {
		return AssignedPlan{}, fmt.Errorf("источник плана недоступен: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return AssignedPlan{}, ErrInternNotFound
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return AssignedPlan{}, fmt.Errorf("источник плана вернул HTTP %d", response.StatusCode)
	}
	var plan AssignedPlan
	if err := json.NewDecoder(response.Body).Decode(&plan); err != nil {
		return AssignedPlan{}, fmt.Errorf("прочитать план: %w", err)
	}
	return plan, nil
}
