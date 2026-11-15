package main

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrInternNotFound = errors.New("стажёр не найден")
	ErrInvalidPlan    = errors.New("источник вернул некорректный план")
)

type AssignedStep struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Detail     string `json:"detail"`
	ActionType string `json:"actionType"`
	Resource   string `json:"resource,omitempty"`
	Completed  bool   `json:"completed"`
}

// Mentor is assigned by the external onboarding source, never by the browser.
type Mentor struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Channel string `json:"channel"`
}

type AssignedPlan struct {
	PlanID   string         `json:"planId"`
	InternID string         `json:"internId"`
	Intern   string         `json:"intern"`
	Position string         `json:"position"`
	Role     string         `json:"role"`
	Team     string         `json:"team"`
	Mentor   Mentor         `json:"mentor"`
	Steps    []AssignedStep `json:"steps"`
}

type OnboardingSource interface {
	GetPlan(ctx context.Context, internID string) (AssignedPlan, error)
}

// GetAssignedPlan combines the external assignment with the current ticket state.
// The source owns step content; the ticket service owns access approval.
func (a *App) GetAssignedPlan(ctx context.Context, internID string) (Plan, error) {
	assigned, err := a.source.GetPlan(ctx, internID)
	if err != nil {
		return Plan{}, err
	}
	if assigned.InternID != internID || assigned.PlanID == "" || assigned.Intern == "" ||
		assigned.Position == "" || assigned.Team == "" || len(assigned.Steps) == 0 ||
		assigned.Mentor.ID == "" || assigned.Mentor.Name == "" || assigned.Mentor.Channel == "" {
		return Plan{}, ErrInvalidPlan
	}

	var access *AssignedStep
	seen := make(map[string]struct{}, len(assigned.Steps))
	for i := range assigned.Steps {
		step := &assigned.Steps[i]
		if step.ID == "" || step.Title == "" {
			return Plan{}, ErrInvalidPlan
		}
		if _, exists := seen[step.ID]; exists {
			return Plan{}, ErrInvalidPlan
		}
		seen[step.ID] = struct{}{}
		switch step.ActionType {
		case "none":
		case "request_access":
			if access != nil || step.Resource == "" {
				return Plan{}, ErrInvalidPlan
			}
			access = step
		default:
			return Plan{}, ErrInvalidPlan
		}
	}

	var request *Request
	if access != nil {
		request, err = a.tickets.GetCurrent(ctx, internID, assigned.PlanID, access.ID)
		if err != nil {
			return Plan{}, fmt.Errorf("получить статус заявки: %w", err)
		}
		if request != nil && (request.InternID != internID || request.PlanID != assigned.PlanID ||
			request.StepID != access.ID || request.Resource != access.Resource ||
			(request.Status != "pending" && request.Status != "approved")) {
			return Plan{}, fmt.Errorf("%w: заявка не соответствует назначенному шагу", ErrInvalidPlan)
		}
	}

	plan := Plan{
		PlanID:   assigned.PlanID,
		Intern:   assigned.Intern,
		Position: assigned.Position,
		Role:     assigned.Role,
		Team:     assigned.Team,
		Mentor:   assigned.Mentor,
		Steps:    make([]Step, 0, len(assigned.Steps)),
		Request:  request,
	}
	blocked := false
	for _, step := range assigned.Steps {
		status := "locked"
		switch {
		case step.ActionType == "request_access" && request != nil && request.Status == "approved":
			status = "done"
		case step.ActionType != "request_access" && step.Completed:
			status = "done"
		case !blocked && step.ActionType == "request_access" && request != nil:
			status = "pending"
		case !blocked:
			status = "current"
		}
		if status == "done" {
			plan.Progress++
		} else {
			blocked = true
		}
		plan.Steps = append(plan.Steps, Step{
			ID: step.ID, Title: step.Title, Detail: step.Detail,
			ActionType: step.ActionType, Resource: step.Resource, Status: status,
		})
	}
	return plan, nil
}
