package agent

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
)

// SafetyRule defines a rule that can block or allow an agent action.
type SafetyRule struct {
	Name        string
	Description string
	Check       func(agentName, action, target string) error // returns error to block
}

// SafetyChecker evaluates agent actions against registered rules.
type SafetyChecker struct {
	rules []SafetyRule
}

// NewSafetyChecker creates a SafetyChecker with built-in default rules.
func NewSafetyChecker() *SafetyChecker {
	s := &SafetyChecker{}
	s.addBuiltInRules()
	return s
}

// AddRule registers an additional safety rule.
func (s *SafetyChecker) AddRule(rule SafetyRule) {
	s.rules = append(s.rules, rule)
}

// Check evaluates all rules against the given action. Returns the first blocking error, or nil.
func (s *SafetyChecker) Check(agentName, action, target string) error {
	for _, rule := range s.rules {
		if err := rule.Check(agentName, action, target); err != nil {
			log.Printf("safety: rule %q blocked agent %q action %q on %q: %v", rule.Name, agentName, action, target, err)
			return fmt.Errorf("safety rule %q: %w", rule.Name, err)
		}
	}
	return nil
}

// Rules returns a copy of the current rules list (for inspection/display).
func (s *SafetyChecker) Rules() []SafetyRule {
	out := make([]SafetyRule, len(s.rules))
	copy(out, s.rules)
	return out
}

func (s *SafetyChecker) addBuiltInRules() {
	s.AddRule(SafetyRule{
		Name:        "system_protection",
		Description: "Block commands that could damage the system (rm -rf on critical paths, dropping databases, etc.)",
		Check:       systemProtectionCheck,
	})
	s.AddRule(SafetyRule{
		Name:        "production_protection",
		Description: "Block actions targeting production environments; warn on staging",
		Check:       productionProtectionCheck,
	})
	s.AddRule(SafetyRule{
		Name:        "agent_scope",
		Description: "Agents can only use tools they're registered for (workers can't write memory, only orchestrator controls IoT)",
		Check:       agentScopeCheck,
	})
	s.AddRule(SafetyRule{
		Name:        "approval_gate",
		Description: "Certain actions require human approval (schema changes, config changes, IoT device removal)",
		Check:       approvalGateCheck,
	})
}

// --- Built-in rule implementations ---

var criticalPaths = []string{"/", "/etc", "/usr", "/System", "/var", "/bin", "/sbin", "/lib", "/opt"}

func systemProtectionCheck(_, action, target string) error {
	lowerAction := strings.ToLower(action)
	lowerTarget := strings.ToLower(target)

	// Block rm -rf on critical paths
	if strings.Contains(lowerAction, "rm") && (strings.Contains(lowerAction, "-rf") || strings.Contains(lowerAction, "-fr")) {
		for _, cp := range criticalPaths {
			if strings.HasPrefix(lowerTarget, strings.ToLower(cp)) || lowerTarget == strings.ToLower(cp) {
				return fmt.Errorf("refusing rm -rf on critical path %q", target)
			}
			if matched, _ := filepath.Match(cp+"/*", target); matched {
				return fmt.Errorf("refusing rm -rf on critical path pattern matching %q", target)
			}
		}
	}

	// Block dropping/wiping databases
	if strings.Contains(lowerAction, "drop") && (strings.Contains(lowerTarget, "database") || strings.Contains(lowerTarget, "db")) {
		return fmt.Errorf("refusing to drop database %q", target)
	}

	// Block overwriting critical config files
	criticalConfigPrefixes := []string{"/etc/", "/system/", "/usr/local/etc/"}
	for _, prefix := range criticalConfigPrefixes {
		if strings.HasPrefix(lowerTarget, prefix) && (strings.Contains(lowerAction, "write") || strings.Contains(lowerAction, "overwrite")) {
			return fmt.Errorf("refusing to overwrite critical config at %q", target)
		}
	}

	// Block killing system processes
	if strings.Contains(lowerAction, "kill") {
		lowerTargetProc := strings.ToLower(target)
		systemProcs := []string{"init", "systemd", "launchd", "kernel", "kworker", "syslog", "sshd", "cron"}
		for _, sp := range systemProcs {
			if lowerTargetProc == sp || strings.HasPrefix(lowerTargetProc, sp) {
				return fmt.Errorf("refusing to kill system process %q", target)
			}
		}
	}

	return nil
}

func productionProtectionCheck(_, action, target string) error {
	lowerTarget := strings.ToLower(target)

	// Block actions targeting production
	if strings.Contains(lowerTarget, "prod") || strings.Contains(lowerTarget, "production") {
		return fmt.Errorf("action %q targets production environment %q — requires explicit approval", action, target)
	}

	// Warn (log but don't block) on staging
	if strings.Contains(lowerTarget, "staging") {
		log.Printf("safety: warning — action %q targets staging environment %q", action, target)
	}

	return nil
}

func agentScopeCheck(agentName, action, target string) error {
	// Workers shouldn't have write access to memory_store or memory_config
	if strings.HasPrefix(agentName, "worker") {
		if action == "memory_store" || action == "memory_config" {
			return fmt.Errorf("agent %q cannot use tool %q — only orchestrator/manager agents have write access to memory", agentName, action)
		}
		// Only the orchestrator/manager can control IoT devices
		if action == "matter" && (strings.Contains(target, "add") || strings.Contains(target, "remove") || strings.Contains(target, "signal")) {
			return fmt.Errorf("agent %q cannot control IoT devices — only orchestrator/manager agents can", agentName)
		}
	}

	return nil
}

func approvalGateCheck(_, action, target string) error {
	lowerAction := strings.ToLower(action)
	lowerTarget := strings.ToLower(target)

	// Database schema changes
	if strings.Contains(lowerAction, "schema") || strings.Contains(lowerAction, "migration") {
		return fmt.Errorf("database schema change on %q requires human approval", target)
	}

	// Agent configuration changes
	if strings.Contains(lowerAction, "config") && (strings.Contains(lowerTarget, "agent") || strings.Contains(lowerAction, "agent_config")) {
		return fmt.Errorf("agent configuration change on %q requires human approval", target)
	}

	// IoT device removal
	if (lowerAction == "matter" || strings.Contains(lowerAction, "matter")) && strings.Contains(strings.ToLower(target), "remove") {
		return fmt.Errorf("IoT device removal on %q requires human approval", target)
	}

	return nil
}