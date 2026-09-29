package interlock

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/wyw14/solo-039/internal/model"
)

// RuleCriteria 是联锁判据的可声明版本。零值字段表示该项不参与判定，
// 因此草案可以只收紧某一条判据而不必重复全部内容。
type RuleCriteria struct {
	// MinimumEvidence 要求的独立证据条数。
	MinimumEvidence int
	// RequireIndependent 为 true 时证据必须全部来自不同来源。
	RequireIndependent bool
	// RequiredSources 必须全部出现的证据来源（如 analyzer、column-pressure）。
	RequiredSources []string
	// AllowedPhases 允许放行的阶段；为空时不限制阶段。
	AllowedPhases []model.Phase
	// Grade 为空表示适用于全部品级，否则仅适用于该品级。
	Grade string
}

// Rule 是某一版联锁判据的不可变快照。
type Rule struct {
	Version     int
	Grade       string
	Criteria    RuleCriteria
	EffectiveAt int64 // unix 秒，自该时刻起在线判定可选用
	PublishedAt int64
	ApprovedBy  string
	Superseded  bool
}

// RuleBook 保存联锁规则的全部历史版本。规则只追加、不就地修改，
// 任何时刻在线判定都通过 ActiveAt/VersionFor 选取一个已发布版本，
// 未发布的草案永远不进入规则簿。
type RuleBook struct {
	mu      sync.RWMutex
	ordered []Rule
	byGrade map[string][]Rule
	next    int
}

func NewRuleBook() *RuleBook {
	return &RuleBook{byGrade: make(map[string][]Rule), next: 1}
}

// Bootstrap 装入系统出厂基线版本（等价于原 Coordinator：
// 至少 2 条、来源独立、阶段 on-spec）。
func (b *RuleBook) Bootstrap(now time.Time) Rule {
	return Rule{
		Version:     1,
		Criteria:    BaselineCriteria(),
		EffectiveAt: 0,
		PublishedAt: now.Unix(),
		ApprovedBy:  "system",
	}
}

func BaselineCriteria() RuleCriteria {
	return RuleCriteria{
		MinimumEvidence:    2,
		RequireIndependent: true,
		AllowedPhases:      []model.Phase{model.OnSpec},
	}
}

// Seed 用一条已发布规则初始化规则簿（通常是 Bootstrap 基线）。
func (b *RuleBook) Seed(rule Rule) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.ordered) > 0 {
		return errors.New("rule book already seeded")
	}
	if rule.Version <= 0 {
		return errors.New("rule version is invalid")
	}
	b.ordered = append(b.ordered, rule)
	b.byGrade[rule.Grade] = append(b.byGrade[rule.Grade], rule)
	b.next = rule.Version + 1
	return nil
}

// Publish 追加一条已审批规则。新版本不修改旧版本；同一品级上一条已发布规则
// 标记为被取代，但保留在簿中供历史解释。
func (b *RuleBook) Publish(candidate Rule, now time.Time) (Rule, error) {
	if candidate.ApprovedBy == "" {
		return Rule{}, errors.New("rule publish requires approval")
	}
	if err := ValidateCriteria(candidate.Criteria); err != nil {
		return Rule{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	candidate.Version = b.next
	b.next++
	if candidate.PublishedAt == 0 {
		candidate.PublishedAt = now.Unix()
	}
	if candidate.EffectiveAt == 0 {
		candidate.EffectiveAt = candidate.PublishedAt
	}
	for i := range b.byGrade[candidate.Grade] {
		b.byGrade[candidate.Grade][i].Superseded = true
	}
	b.ordered = append(b.ordered, candidate)
	b.byGrade[candidate.Grade] = append(b.byGrade[candidate.Grade], candidate)
	return candidate, nil
}

func ValidateCriteria(c RuleCriteria) error {
	if c.MinimumEvidence < 0 {
		return errors.New("minimum evidence cannot be negative")
	}
	if c.MinimumEvidence == 0 && !c.RequireIndependent && len(c.RequiredSources) == 0 && len(c.AllowedPhases) == 0 {
		return errors.New("rule criteria is incomplete")
	}
	seen := make(map[string]struct{}, len(c.RequiredSources))
	for _, source := range c.RequiredSources {
		if source == "" {
			return errors.New("required source is blank")
		}
		if _, ok := seen[source]; ok {
			return fmt.Errorf("required source duplicated: %s", source)
		}
		seen[source] = struct{}{}
	}
	seenPhase := make(map[model.Phase]struct{}, len(c.AllowedPhases))
	for _, phase := range c.AllowedPhases {
		if phase == "" {
			return errors.New("allowed phase is blank")
		}
		if _, ok := seenPhase[phase]; ok {
			return fmt.Errorf("allowed phase duplicated: %s", phase)
		}
		seenPhase[phase] = struct{}{}
	}
	return nil
}

// ActiveAt 返回某品级在 at 时刻应当生效的规则：已发布且生效时间不晚于 at 的
// 最新版本。找不到品级专用规则时回落到全局规则（Grade 为空）。
func (b *RuleBook) ActiveAt(grade string, at time.Time) (Rule, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if rule, ok := latestEffective(b.byGrade[grade], at.Unix()); ok {
		return rule, true
	}
	return latestEffective(b.byGrade[""], at.Unix())
}

// Version 按版本号取规则，用于按当时版本重新解释历史决定。
func (b *RuleBook) Version(version int) (Rule, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, rule := range b.ordered {
		if rule.Version == version {
			return rule, true
		}
	}
	return Rule{}, false
}

// Versions 返回规则簿的只读快照（按版本顺序）。
func (b *RuleBook) Versions() []Rule {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return append([]Rule(nil), b.ordered...)
}

func latestEffective(rules []Rule, at int64) (Rule, bool) {
	var found Rule
	for _, rule := range rules {
		if rule.EffectiveAt > at {
			continue
		}
		if found.Version == 0 || rule.Version > found.Version {
			found = rule
		}
	}
	return found, found.Version != 0
}

// RuleEngine 按给定版本规则执行纯函数判定，不持有可变状态，
// 因此同一规则可同时用于在线评估、沙盒回放和历史重算。
type RuleEngine struct{}

func NewRuleEngine() *RuleEngine { return &RuleEngine{} }

type DecisionInput struct {
	Proofs   []model.Proof
	Campaign model.Campaign
	At       time.Time
}

// Evaluate 依据 rule 给出许可，Reasons 与既有 Permit 语义保持一致。
func (e *RuleEngine) Evaluate(rule Rule, in DecisionInput) Permit {
	p := Permit{Evidence: append([]model.Proof(nil), in.Proofs...), RuleVersion: rule.Version}
	criteria := rule.Criteria

	if criteria.MinimumEvidence > 0 && len(in.Proofs) < criteria.MinimumEvidence {
		p.Reasons = append(p.Reasons, "insufficient evidence")
		return p
	}
	if criteria.RequireIndependent {
		if !independentProofs(in.Proofs) {
			p.Reasons = append(p.Reasons, "evidence is not independent")
			return p
		}
	}
	if len(criteria.RequiredSources) > 0 && !hasSources(in.Proofs, criteria.RequiredSources) {
		missing := missingSources(in.Proofs, criteria.RequiredSources)
		p.Reasons = append(p.Reasons, "missing required evidence: "+joinSources(missing))
		return p
	}
	if len(criteria.AllowedPhases) > 0 && !phaseAllowed(in.Campaign.Phase, criteria.AllowedPhases) {
		p.Reasons = append(p.Reasons, "campaign phase is not permitted")
		return p
	}
	p.Allowed = true
	return p
}

func independentProofs(proofs []model.Proof) bool {
	seen := make(map[string]struct{})
	count := 0
	for _, proof := range proofs {
		if !proof.Independent {
			return false
		}
		if _, ok := seen[proof.Source]; ok {
			return false
		}
		seen[proof.Source] = struct{}{}
		count++
	}
	return count == len(proofs)
}

func hasSources(proofs []model.Proof, required []string) bool {
	present := make(map[string]struct{})
	for _, proof := range proofs {
		present[proof.Source] = struct{}{}
	}
	for _, source := range required {
		if _, ok := present[source]; !ok {
			return false
		}
	}
	return true
}

func missingSources(proofs []model.Proof, required []string) []string {
	present := make(map[string]struct{})
	for _, proof := range proofs {
		present[proof.Source] = struct{}{}
	}
	missing := make([]string, 0)
	for _, source := range required {
		if _, ok := present[source]; !ok {
			missing = append(missing, source)
		}
	}
	sort.Strings(missing)
	return missing
}

func phaseAllowed(phase model.Phase, allowed []model.Phase) bool {
	for _, candidate := range allowed {
		if phase == candidate {
			return true
		}
	}
	return false
}

func joinSources(sources []string) string {
	if len(sources) == 0 {
		return ""
	}
	sorted := append([]string(nil), sources...)
	sort.Strings(sorted)
	out := ""
	for i, source := range sorted {
		if i > 0 {
			out += ","
		}
		out += source
	}
	return out
}
