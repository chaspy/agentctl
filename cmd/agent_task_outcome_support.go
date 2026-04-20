package cmd

import (
	"database/sql"
	"fmt"

	"github.com/chaspy/agentctl/internal/store"
)

var allowedAgentTaskOutcomeStatuses = map[string]bool{
	"completed": true,
	"failed":    true,
	"cancelled": true,
}

type agentTaskOutcomePlan struct {
	Outcome *store.AgentTaskOutcome
	Session *store.Session
}

func buildAgentTaskOutcomePlan(
	db *sql.DB,
	attempt *store.AgentTaskAttempt,
	statusOverride string,
	summaryOverride string,
	prNumberOverride int,
	prURLOverride string,
	prStateOverride string,
	commitOverride string,
	failureCategoryOverride string,
	failureReasonOverride string,
	sourceOverride string,
) (*agentTaskOutcomePlan, error) {
	session, err := resolveAgentTaskAttemptSession(db, attempt)
	if err != nil {
		return nil, err
	}

	status := statusOverride
	if status == "" {
		switch attempt.Status {
		case "failed":
			status = "failed"
		case "cancelled":
			status = "cancelled"
		default:
			return nil, fmt.Errorf("attempt %d is %q, explicit --status is required", attempt.ID, attempt.Status)
		}
	}
	if !allowedAgentTaskOutcomeStatuses[status] {
		return nil, fmt.Errorf("unsupported outcome status %q", status)
	}

	resultSummary := summaryOverride
	if resultSummary == "" {
		if session != nil && session.TaskSummary != "" {
			resultSummary = session.TaskSummary
		} else {
			resultSummary = attempt.Summary
		}
	}

	branch := attempt.Branch
	if branch == "" && session != nil {
		branch = session.GitBranch
	}
	sessionName := attempt.SessionName
	if sessionName == "" && session != nil {
		sessionName = session.ZellijSession
	}

	prNumber := 0
	prURL := ""
	prState := ""
	if session != nil {
		prNumber = session.PRNumber
		prURL = session.PRURL
		prState = session.PRState
	}
	if prNumberOverride > 0 {
		prNumber = prNumberOverride
	}
	if prURLOverride != "" {
		prURL = prURLOverride
	}
	if prStateOverride != "" {
		prState = prStateOverride
	}

	failureCategory := failureCategoryOverride
	failureReason := failureReasonOverride
	if failureReason == "" && status == "failed" {
		failureReason = attempt.FailureReason
	}
	if status != "failed" {
		failureCategory = ""
		failureReason = ""
	}

	source := sourceOverride
	if source == "" {
		if session != nil {
			source = "session"
		} else {
			source = "manual"
		}
	}

	return &agentTaskOutcomePlan{
		Outcome: &store.AgentTaskOutcome{
			AttemptID:        attempt.ID,
			DecisionID:       attempt.DecisionID,
			AgentTaskName:    attempt.AgentTaskName,
			RepoRef:          attempt.RepoRef,
			Repository:       attempt.Repository,
			TaskType:         attempt.TaskType,
			Risk:             attempt.Risk,
			Agent:            attempt.Agent,
			Branch:           branch,
			SessionName:      sessionName,
			ManagedSessionID: attempt.ManagedSessionID,
			Status:           status,
			ResultSummary:    resultSummary,
			PRNumber:         prNumber,
			PRURL:            prURL,
			PRState:          prState,
			CommitSHA:        commitOverride,
			FailureCategory:  failureCategory,
			FailureReason:    failureReason,
			Source:           source,
		},
		Session: session,
	}, nil
}

func resolveAgentTaskAttemptSession(db *sql.DB, attempt *store.AgentTaskAttempt) (*store.Session, error) {
	if attempt.ManagedSessionID == "" {
		return nil, nil
	}
	session, err := store.GetSession(db, attempt.ManagedSessionID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("loading managed session %q: %w", attempt.ManagedSessionID, err)
	}
	return session, nil
}
