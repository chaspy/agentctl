package cmd

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	resourceKindManagedRepo        = "ManagedRepo"
	resourceKindAgentOpsEcosystem  = "AgentOpsEcosystem"
	resourceKindSelfHostingPolicy  = "SelfHostingPolicy"
	resourceKindRoutingPolicy      = "RoutingPolicy"
	resourceKindReviewPolicy       = "ReviewPolicy"
	resourceKindApprovalPolicy     = "ApprovalPolicy"
	resourceKindBenchmarkPolicy    = "BenchmarkPolicy"
	resourceKindExperienceProposal = "ExperienceProposal"
	resourceKindReleaseGate        = "ReleaseGate"
)

type applyHeader struct {
	Kind string `yaml:"kind"`
}

type manifestValidationResult struct {
	Path   string   `json:"path,omitempty"`
	Kind   string   `json:"kind"`
	Name   string   `json:"name,omitempty"`
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors,omitempty"`
}

type managedRepoManifest struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec managedRepoSpec `yaml:"spec"`
}

type managedRepoSelfHostingSpec struct {
	CanModifyDocs                    bool     `yaml:"canModifyDocs" json:"canModifyDocs,omitempty"`
	CanModifyOpsSpecs                bool     `yaml:"canModifyOpsSpecs" json:"canModifyOpsSpecs,omitempty"`
	CanModifyRuntimeCode             bool     `yaml:"canModifyRuntimeCode" json:"canModifyRuntimeCode,omitempty"`
	CanModifyApprovalPolicy          bool     `yaml:"canModifyApprovalPolicy" json:"canModifyApprovalPolicy,omitempty"`
	RequiresHumanApprovalBeforeMerge bool     `yaml:"requiresHumanApprovalBeforeMerge" json:"requiresHumanApprovalBeforeMerge,omitempty"`
	RequiresExtraReviewFor           []string `yaml:"requiresExtraReviewFor" json:"requiresExtraReviewFor,omitempty"`
}

type managedRepoSpec struct {
	Repo                      string                     `yaml:"repo" json:"repo"`
	Tier                      string                     `yaml:"tier" json:"tier,omitempty"`
	Role                      string                     `yaml:"role" json:"role"`
	Visibility                string                     `yaml:"visibility" json:"visibility"`
	RepoContractPath          string                     `yaml:"repoContractPath" json:"repoContractPath,omitempty"`
	DefaultRoutingPolicyRef   string                     `yaml:"defaultRoutingPolicyRef" json:"defaultRoutingPolicyRef,omitempty"`
	DefaultReviewPolicyRef    string                     `yaml:"defaultReviewPolicyRef" json:"defaultReviewPolicyRef,omitempty"`
	DefaultApprovalPolicyRef  string                     `yaml:"defaultApprovalPolicyRef" json:"defaultApprovalPolicyRef,omitempty"`
	DefaultBenchmarkPolicyRef string                     `yaml:"defaultBenchmarkPolicyRef" json:"defaultBenchmarkPolicyRef,omitempty"`
	DefaultReleaseGateRef     string                     `yaml:"defaultReleaseGateRef" json:"defaultReleaseGateRef,omitempty"`
	SelfHosting               managedRepoSelfHostingSpec `yaml:"selfHosting" json:"selfHosting,omitempty"`
	Notes                     string                     `yaml:"notes" json:"notes,omitempty"`
}

type agentOpsEcosystemManifest struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Owner        string `yaml:"owner"`
		ControlPlane struct {
			RepoRef          string `yaml:"repoRef"`
			ObservabilityRef string `yaml:"observabilityRef"`
		} `yaml:"controlPlane"`
		Console struct {
			RepoRef string `yaml:"repoRef"`
		} `yaml:"console"`
		ManagedRepos []string `yaml:"managedRepos"`
		SelfHosting  struct {
			Enabled   bool   `yaml:"enabled"`
			PolicyRef string `yaml:"policyRef"`
		} `yaml:"selfHosting"`
		Safety struct {
			BreakGlassRunbook        string   `yaml:"breakGlassRunbook"`
			HumanApprovalRequiredFor []string `yaml:"humanApprovalRequiredFor"`
		} `yaml:"safety"`
	} `yaml:"spec"`
}

type selfHostingPolicyManifest struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Principle string `yaml:"principle"`
		Tiers     []struct {
			Name       string   `yaml:"name"`
			Repos      []string `yaml:"repos"`
			Automation struct {
				Enabled                  bool `yaml:"enabled"`
				AllowPullRequestCreation bool `yaml:"allowPullRequestCreation"`
				AllowDirectPush          bool `yaml:"allowDirectPush"`
				AllowAutoMerge           bool `yaml:"allowAutoMerge"`
				RequireHumanApproval     any  `yaml:"requireHumanApproval"`
			} `yaml:"automation"`
		} `yaml:"tiers"`
		ForbiddenWithoutHumanApproval []string `yaml:"forbiddenWithoutHumanApproval"`
		AllowedLowRiskChanges         []string `yaml:"allowedLowRiskChanges"`
		Audit                         struct {
			RequireDecisionLog   bool `yaml:"requireDecisionLog"`
			RequireAttemptLog    bool `yaml:"requireAttemptLog"`
			RequireOutcomeLog    bool `yaml:"requireOutcomeLog"`
			RequirePolicyVersion bool `yaml:"requirePolicyVersion"`
		} `yaml:"audit"`
	} `yaml:"spec"`
}

type routingPolicyManifest struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Version any `yaml:"version"`
		Rules   []struct {
			Prefer string `yaml:"prefer"`
		} `yaml:"rules"`
	} `yaml:"spec"`
}

type reviewPolicyManifest struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Triggers []string `yaml:"triggers"`
		Checks   []struct {
			Type string `yaml:"type"`
		} `yaml:"checks"`
		Gates []string `yaml:"gates"`
	} `yaml:"spec"`
}

type approvalPolicyManifest struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Version any `yaml:"version"`
		Rules   []struct {
			RequiresHumanApproval bool     `yaml:"requiresHumanApproval"`
			RequiredBefore        []string `yaml:"requiredBefore"`
		} `yaml:"rules"`
	} `yaml:"spec"`
}

type benchmarkPolicyManifest struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Version        any      `yaml:"version"`
		Datasets       []string `yaml:"datasets"`
		EvaluationAxes []string `yaml:"evaluationAxes"`
		OutputFields   []string `yaml:"outputFields"`
	} `yaml:"spec"`
}

type experienceProposalManifest struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		RepoRef            string   `yaml:"repoRef"`
		Intent             string   `yaml:"intent"`
		AcceptanceCriteria []string `yaml:"acceptanceCriteria"`
	} `yaml:"spec"`
}

type releaseGateManifest struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		RepoRef          string   `yaml:"repoRef"`
		RequiredEvidence []string `yaml:"requiredEvidence"`
		RequiredChecks   []string `yaml:"requiredChecks"`
		Approval         struct {
			HumanRequired bool `yaml:"humanRequired"`
		} `yaml:"approval"`
	} `yaml:"spec"`
}

func detectManifestKind(content []byte) (string, error) {
	var header applyHeader
	if err := yaml.Unmarshal(content, &header); err != nil {
		return "", fmt.Errorf("parse manifest header: %w", err)
	}
	kind := strings.TrimSpace(header.Kind)
	if kind == "" {
		return "", fmt.Errorf("manifest kind is required")
	}
	return kind, nil
}

func validateManifestContent(content []byte) manifestValidationResult {
	kind, err := detectManifestKind(content)
	if err != nil {
		return manifestValidationResult{
			Kind:   "",
			Valid:  false,
			Errors: []string{err.Error()},
		}
	}

	switch kind {
	case resourceKindManagedRepo:
		return validateManagedRepoManifest(content)
	case resourceKindAgentOpsEcosystem:
		return validateAgentOpsEcosystemManifest(content)
	case resourceKindSelfHostingPolicy:
		return validateSelfHostingPolicyManifest(content)
	case resourceKindRoutingPolicy:
		return validateRoutingPolicyManifest(content)
	case resourceKindReviewPolicy:
		return validateReviewPolicyManifest(content)
	case resourceKindApprovalPolicy:
		return validateApprovalPolicyManifest(content)
	case resourceKindBenchmarkPolicy:
		return validateBenchmarkPolicyManifest(content)
	case resourceKindExperienceProposal:
		return validateExperienceProposalManifest(content)
	case resourceKindReleaseGate:
		return validateReleaseGateManifest(content)
	default:
		return manifestValidationResult{
			Kind:   kind,
			Valid:  false,
			Errors: []string{fmt.Sprintf("unsupported kind %q", kind)},
		}
	}
}

func validateManagedRepoManifest(content []byte) manifestValidationResult {
	var manifest managedRepoManifest
	if err := yaml.Unmarshal(content, &manifest); err != nil {
		return manifestValidationResult{
			Kind:   resourceKindManagedRepo,
			Valid:  false,
			Errors: []string{fmt.Sprintf("parse ManagedRepo: %v", err)},
		}
	}

	result := manifestValidationResult{
		Kind: resourceKindManagedRepo,
		Name: strings.TrimSpace(manifest.Metadata.Name),
	}
	var errs []string
	if result.Name == "" {
		errs = append(errs, "metadata.name is required")
	}
	if strings.TrimSpace(manifest.Spec.Repo) == "" {
		errs = append(errs, "spec.repo is required")
	} else if _, err := normalizeManagedRepoRef(manifest.Spec.Repo); err != nil {
		errs = append(errs, err.Error())
	}
	if strings.TrimSpace(manifest.Spec.Role) == "" {
		errs = append(errs, "spec.role is required")
	}
	switch strings.TrimSpace(manifest.Spec.Visibility) {
	case "public", "private", "internal":
	default:
		errs = append(errs, "spec.visibility must be one of public, private, internal")
	}
	if tier := strings.TrimSpace(manifest.Spec.Tier); tier != "" {
		switch tier {
		case "bootstrap", "control-plane", "workload":
		default:
			errs = append(errs, "spec.tier must be one of bootstrap, control-plane, workload")
		}
	}
	result.Valid = len(errs) == 0
	result.Errors = errs
	return result
}

func validateAgentOpsEcosystemManifest(content []byte) manifestValidationResult {
	var manifest agentOpsEcosystemManifest
	if err := yaml.Unmarshal(content, &manifest); err != nil {
		return manifestValidationResult{
			Kind:   resourceKindAgentOpsEcosystem,
			Valid:  false,
			Errors: []string{fmt.Sprintf("parse AgentOpsEcosystem: %v", err)},
		}
	}
	result := manifestValidationResult{
		Kind: resourceKindAgentOpsEcosystem,
		Name: strings.TrimSpace(manifest.Metadata.Name),
	}
	var errs []string
	if result.Name == "" {
		errs = append(errs, "metadata.name is required")
	}
	if strings.TrimSpace(manifest.Spec.Owner) == "" {
		errs = append(errs, "spec.owner is required")
	}
	if strings.TrimSpace(manifest.Spec.ControlPlane.RepoRef) == "" {
		errs = append(errs, "spec.controlPlane.repoRef is required")
	}
	if strings.TrimSpace(manifest.Spec.Console.RepoRef) == "" {
		errs = append(errs, "spec.console.repoRef is required")
	}
	if len(manifest.Spec.ManagedRepos) == 0 {
		errs = append(errs, "spec.managedRepos must contain at least one repo")
	}
	if strings.TrimSpace(manifest.Spec.SelfHosting.PolicyRef) == "" {
		errs = append(errs, "spec.selfHosting.policyRef is required")
	}
	result.Valid = len(errs) == 0
	result.Errors = errs
	return result
}

func validateSelfHostingPolicyManifest(content []byte) manifestValidationResult {
	var manifest selfHostingPolicyManifest
	if err := yaml.Unmarshal(content, &manifest); err != nil {
		return manifestValidationResult{
			Kind:   resourceKindSelfHostingPolicy,
			Valid:  false,
			Errors: []string{fmt.Sprintf("parse SelfHostingPolicy: %v", err)},
		}
	}
	result := manifestValidationResult{
		Kind: resourceKindSelfHostingPolicy,
		Name: strings.TrimSpace(manifest.Metadata.Name),
	}
	var errs []string
	if result.Name == "" {
		errs = append(errs, "metadata.name is required")
	}
	if strings.TrimSpace(manifest.Spec.Principle) == "" {
		errs = append(errs, "spec.principle is required")
	}
	if len(manifest.Spec.Tiers) == 0 {
		errs = append(errs, "spec.tiers must contain at least one tier")
	}
	for i, tier := range manifest.Spec.Tiers {
		if strings.TrimSpace(tier.Name) == "" {
			errs = append(errs, fmt.Sprintf("spec.tiers[%d].name is required", i))
		}
	}
	result.Valid = len(errs) == 0
	result.Errors = errs
	return result
}

func validateRoutingPolicyManifest(content []byte) manifestValidationResult {
	var manifest routingPolicyManifest
	if err := yaml.Unmarshal(content, &manifest); err != nil {
		return manifestValidationResult{
			Kind:   resourceKindRoutingPolicy,
			Valid:  false,
			Errors: []string{fmt.Sprintf("parse RoutingPolicy: %v", err)},
		}
	}
	result := manifestValidationResult{Kind: resourceKindRoutingPolicy, Name: strings.TrimSpace(manifest.Metadata.Name)}
	var errs []string
	if result.Name == "" {
		errs = append(errs, "metadata.name is required")
	}
	if len(manifest.Spec.Rules) == 0 {
		errs = append(errs, "spec.rules must contain at least one rule")
	}
	for i, rule := range manifest.Spec.Rules {
		if strings.TrimSpace(rule.Prefer) == "" {
			errs = append(errs, fmt.Sprintf("spec.rules[%d].prefer is required", i))
		}
	}
	result.Valid = len(errs) == 0
	result.Errors = errs
	return result
}

func validateReviewPolicyManifest(content []byte) manifestValidationResult {
	var manifest reviewPolicyManifest
	if err := yaml.Unmarshal(content, &manifest); err != nil {
		return manifestValidationResult{
			Kind:   resourceKindReviewPolicy,
			Valid:  false,
			Errors: []string{fmt.Sprintf("parse ReviewPolicy: %v", err)},
		}
	}
	result := manifestValidationResult{Kind: resourceKindReviewPolicy, Name: strings.TrimSpace(manifest.Metadata.Name)}
	var errs []string
	if result.Name == "" {
		errs = append(errs, "metadata.name is required")
	}
	if len(manifest.Spec.Triggers) == 0 {
		errs = append(errs, "spec.triggers must contain at least one trigger")
	}
	if len(manifest.Spec.Checks) == 0 {
		errs = append(errs, "spec.checks must contain at least one check")
	}
	result.Valid = len(errs) == 0
	result.Errors = errs
	return result
}

func validateApprovalPolicyManifest(content []byte) manifestValidationResult {
	var manifest approvalPolicyManifest
	if err := yaml.Unmarshal(content, &manifest); err != nil {
		return manifestValidationResult{
			Kind:   resourceKindApprovalPolicy,
			Valid:  false,
			Errors: []string{fmt.Sprintf("parse ApprovalPolicy: %v", err)},
		}
	}
	result := manifestValidationResult{Kind: resourceKindApprovalPolicy, Name: strings.TrimSpace(manifest.Metadata.Name)}
	var errs []string
	if result.Name == "" {
		errs = append(errs, "metadata.name is required")
	}
	if len(manifest.Spec.Rules) == 0 {
		errs = append(errs, "spec.rules must contain at least one rule")
	}
	result.Valid = len(errs) == 0
	result.Errors = errs
	return result
}

func validateBenchmarkPolicyManifest(content []byte) manifestValidationResult {
	var manifest benchmarkPolicyManifest
	if err := yaml.Unmarshal(content, &manifest); err != nil {
		return manifestValidationResult{
			Kind:   resourceKindBenchmarkPolicy,
			Valid:  false,
			Errors: []string{fmt.Sprintf("parse BenchmarkPolicy: %v", err)},
		}
	}
	result := manifestValidationResult{Kind: resourceKindBenchmarkPolicy, Name: strings.TrimSpace(manifest.Metadata.Name)}
	var errs []string
	if result.Name == "" {
		errs = append(errs, "metadata.name is required")
	}
	if len(manifest.Spec.Datasets) == 0 {
		errs = append(errs, "spec.datasets must contain at least one dataset")
	}
	if len(manifest.Spec.EvaluationAxes) == 0 {
		errs = append(errs, "spec.evaluationAxes must contain at least one axis")
	}
	if len(manifest.Spec.OutputFields) == 0 {
		errs = append(errs, "spec.outputFields must contain at least one field")
	}
	result.Valid = len(errs) == 0
	result.Errors = errs
	return result
}

func validateExperienceProposalManifest(content []byte) manifestValidationResult {
	var manifest experienceProposalManifest
	if err := yaml.Unmarshal(content, &manifest); err != nil {
		return manifestValidationResult{
			Kind:   resourceKindExperienceProposal,
			Valid:  false,
			Errors: []string{fmt.Sprintf("parse ExperienceProposal: %v", err)},
		}
	}
	result := manifestValidationResult{Kind: resourceKindExperienceProposal, Name: strings.TrimSpace(manifest.Metadata.Name)}
	var errs []string
	if result.Name == "" {
		errs = append(errs, "metadata.name is required")
	}
	if strings.TrimSpace(manifest.Spec.RepoRef) == "" {
		errs = append(errs, "spec.repoRef is required")
	}
	if strings.TrimSpace(manifest.Spec.Intent) == "" {
		errs = append(errs, "spec.intent is required")
	}
	if len(manifest.Spec.AcceptanceCriteria) == 0 {
		errs = append(errs, "spec.acceptanceCriteria must contain at least one criterion")
	}
	result.Valid = len(errs) == 0
	result.Errors = errs
	return result
}

func validateReleaseGateManifest(content []byte) manifestValidationResult {
	var manifest releaseGateManifest
	if err := yaml.Unmarshal(content, &manifest); err != nil {
		return manifestValidationResult{
			Kind:   resourceKindReleaseGate,
			Valid:  false,
			Errors: []string{fmt.Sprintf("parse ReleaseGate: %v", err)},
		}
	}
	result := manifestValidationResult{Kind: resourceKindReleaseGate, Name: strings.TrimSpace(manifest.Metadata.Name)}
	var errs []string
	if result.Name == "" {
		errs = append(errs, "metadata.name is required")
	}
	if strings.TrimSpace(manifest.Spec.RepoRef) == "" {
		errs = append(errs, "spec.repoRef is required")
	}
	if len(manifest.Spec.RequiredEvidence) == 0 {
		errs = append(errs, "spec.requiredEvidence must contain at least one evidence item")
	}
	if len(manifest.Spec.RequiredChecks) == 0 {
		errs = append(errs, "spec.requiredChecks must contain at least one check")
	}
	result.Valid = len(errs) == 0
	result.Errors = errs
	return result
}
