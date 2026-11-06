// Copyright 2026 Zain Haseeb and contributors. Licensed under Apache-2.0. See LICENSE.
// Hand-written novel feature; not generated.

package cli

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/zm2231/skool-cli/internal/client"
	"github.com/zm2231/skool-cli/internal/store"
)

type rosterRow struct {
	ID                 string  `json:"id"`
	Handle             string  `json:"handle"`
	Name               string  `json:"name"`
	Role               string  `json:"role"`
	MembershipState    string  `json:"membership_state"`
	IsActive           bool    `json:"is_active"`
	Email              string  `json:"email"`
	EmailSource        string  `json:"email_source"`
	PriceUSDMo         float64 `json:"price_usd_mo"`
	Model              string  `json:"billing_model"`
	PeriodEnd          string  `json:"period_end"`
	IsTrial            bool    `json:"is_trial"`
	ChurnedAt          string  `json:"churned_at"`
	FreeTrialChurnedAt string  `json:"free_trial_churned_at"`
	VanishedAt         string  `json:"vanished_at"`
	RemovedAt          string  `json:"removed_at"`
	RemovedBy          string  `json:"removed_by"`
	StripeSub          string  `json:"stripe_sub"`
	JoinSource         string  `json:"join_source"`
	Joined             string  `json:"joined"`
}

type rosterMember struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	FirstName  string `json:"firstName"`
	LastName   string `json:"lastName"`
	MbState    string `json:"mbState"`
	VanishedAt string `json:"vanished_at"`
	Member     struct {
		Role        string          `json:"role"`
		ApprovedAt  string          `json:"approvedAt"`
		InviteEmail string          `json:"inviteEmail"`
		Churned     string          `json:"churned"`
		RemovedAt   json.RawMessage `json:"removedAt"`
		RemovedBy   json.RawMessage `json:"removedBy"`
		Metadata    struct {
			Mbme               string `json:"mbme"`
			Mbscpe             int64  `json:"mbscpe"`
			Mmbp               string `json:"mmbp"`
			Msbs               string `json:"msbs"`
			AttrComp           string `json:"attrComp"`
			IsFreeTrial        int    `json:"isFreeTrial"`
			FreeTrialChurnedAt int64  `json:"freeTrialChurnedAt"`
		} `json:"metadata"`
	} `json:"member"`
}

var activeStates = map[string]bool{"active": true, "cancelling": true}

func isActiveState(state string) bool { return activeStates[state] }

// Skool lists one member in several state tabs at once (a live member who also
// has a stale banned/churned tombstone); the highest rank wins the merge.
var stateRank = map[string]int{
	"active":      4,
	"cancelling":  3,
	"banned":      2,
	"churned":     1,
	stateVanished: 0,
}

func statePriority(state string) int { return stateRank[state] }

func unixToISO(sec int64) string {
	if sec <= 0 {
		return ""
	}
	return time.Unix(sec, 0).UTC().Format(time.RFC3339)
}

func jsonScalarString(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return ""
	}
	var str string
	if json.Unmarshal(raw, &str) == nil {
		return str
	}
	return s
}

func newMembersRosterCmd(flags *rootFlags) *cobra.Command {
	var flagCommunity, dbPath, flagStatus string
	var flagCSV, flagRefresh, flagTrialOnly bool
	var flagExpiring int

	cmd := &cobra.Command{
		Use:         "roster",
		Short:       "Decoded admin roster: email, price, subscription period-end, trial, Stripe id, join source",
		Example:     "  skool-pp-cli members roster --community <slug> --json\n  skool-pp-cli members roster --refresh --expiring 3 --json\n  skool-pp-cli members roster --csv --deliver file:./roster.csv",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			c, err := flags.newClient()
			if err != nil {
				return err
			}
			community := strings.TrimSpace(flagCommunity)
			if community == "" && c.Config != nil {
				community = c.Config.TemplateVars["community"]
			}
			if dbPath == "" {
				dbPath = defaultDBPath("skool-pp-cli")
			}
			db, err := store.OpenWithContext(cmd.Context(), dbPath)
			if err != nil {
				return fmt.Errorf("opening local database: %w\nRun 'skool-pp-cli sync' first, or pass --refresh.", err)
			}
			defer db.Close()

			statuses, err := parseStatuses(flagStatus)
			if err != nil {
				return usageErr(err)
			}

			if flagRefresh {
				if community == "" {
					return usageErr(fmt.Errorf("--refresh needs a community: pass --community <slug> or set template_vars.community"))
				}
				if err := refreshMembers(c, db, community, statuses); err != nil {
					return classifyAPIError(err, flags)
				}
			}

			records, err := readMemberRecords(db, community)
			if err != nil {
				return err
			}
			if len(records) == 0 {
				return fmt.Errorf("no members in the local store%s; run with --refresh or 'skool-pp-cli sync --resources members'", communitySuffix(community))
			}

			rows := decodeRoster(records)
			rows = filterRoster(rows, statuses, flagTrialOnly, flagExpiring)
			sort.Slice(rows, func(i, j int) bool { return rows[i].PeriodEnd < rows[j].PeriodEnd })

			if flagCSV {
				return writeRosterCSV(cmd, rows)
			}
			out, err := json.Marshal(rows)
			if err != nil {
				return err
			}
			return printOutput(cmd.OutOrStdout(), out, flags.asJSON || !isTerminal(cmd.OutOrStdout()))
		},
	}

	cmd.Flags().StringVar(&flagCommunity, "community", "", "Community slug (defaults to template_vars.community)")
	cmd.Flags().StringVar(&flagStatus, "status", "active", "Membership states to include: active,cancelling,churned,banned (comma-separated), or all")
	cmd.Flags().BoolVar(&flagRefresh, "refresh", false, "Fetch members live before emitting (otherwise read the local store)")
	cmd.Flags().BoolVar(&flagCSV, "csv", false, "Emit CSV instead of JSON")
	cmd.Flags().BoolVar(&flagTrialOnly, "trial-only", false, "Only members currently on a free trial")
	cmd.Flags().IntVar(&flagExpiring, "expiring", 0, "Only members whose subscription period ends within N days (0 = all)")
	cmd.Flags().StringVar(&dbPath, "db", "", "Database path (default: ~/.local/share/skool-pp-cli/data.db)")
	return cmd
}

// fetchTabs are the membership states Skool exposes as a members-tab query.
// stateVanished is synthetic: it is stamped by the reconcile pass on members
// absent from a full sweep, never fetched from a tab.
var fetchTabs = []string{"active", "cancelling", "churned", "banned"}

const stateVanished = "vanished"

// validStatuses is the filterable set (fetchable tabs plus the synthetic
// vanished). "all" expands to this whole set so a full roster includes the
// members reconcile has demoted.
var validStatuses = append(append([]string(nil), fetchTabs...), stateVanished)

func parseStatuses(spec string) ([]string, error) {
	spec = strings.ToLower(strings.TrimSpace(spec))
	if spec == "" || spec == "all" {
		return append([]string(nil), validStatuses...), nil
	}
	valid := map[string]bool{}
	for _, s := range validStatuses {
		valid[s] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, part := range strings.Split(spec, ",") {
		s := strings.TrimSpace(part)
		if s == "" {
			continue
		}
		if !valid[s] {
			return nil, fmt.Errorf("unknown --status %q (valid: active, cancelling, churned, banned, vanished, all)", s)
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--status resolved to no states")
	}
	return out, nil
}

func refreshMembers(c *client.Client, db *store.Store, community string, statuses []string) error {
	path := replacePathParam("/_next/data/{buildId}/{community}/-/members.json", "community", community)
	type collectedRecord struct {
		raw   json.RawMessage
		state string
	}
	best := map[string]collectedRecord{}
	seenIDs := map[string]struct{}{}
	sweptTabs := map[string]bool{}
	for _, status := range statuses {
		if status == stateVanished {
			continue
		}
		sweptTabs[status] = true
		seen := map[string]struct{}{}
		for page := 1; page <= 100; page++ {
			params := map[string]string{"group": community}
			if status != "active" {
				params["t"] = status
			}
			if page > 1 {
				params["p"] = strconv.Itoa(page)
			}
			raw, err := c.GetWithHeaders(path, params, map[string]string{"x-nextjs-data": "1"})
			if err != nil {
				return err
			}
			items, _ := extractSkoolPageRecords(raw, "members")
			added := 0
			for _, item := range items {
				id := recordIdentity(item)
				if id == "" {
					continue
				}
				if _, dup := seen[id]; dup {
					continue
				}
				seen[id] = struct{}{}
				seenIDs[id] = struct{}{}
				added++
				if cur, ok := best[id]; ok && statePriority(cur.state) >= statePriority(status) {
					continue
				}
				best[id] = collectedRecord{raw: tagMembershipState(item, status), state: status}
			}
			if added == 0 {
				break
			}
		}
	}
	if len(best) > 0 {
		collected := make([]json.RawMessage, 0, len(best))
		for _, rec := range best {
			collected = append(collected, rec.raw)
		}
		if _, _, err := db.UpsertBatch("members", collected); err != nil {
			return err
		}
	}
	fullSweep := true
	for _, tab := range fetchTabs {
		if !sweptTabs[tab] {
			fullSweep = false
			break
		}
	}
	if fullSweep {
		return reconcileVanished(db, community, seenIDs)
	}
	return nil
}

func reconcileVanished(db *store.Store, community string, seen map[string]struct{}) error {
	records, err := readMemberRecords(db, community)
	if err != nil {
		return err
	}
	prior := len(records)
	floor := 40
	if half := prior / 2; half > floor {
		floor = half
	}
	if len(seen) < floor {
		fmt.Fprintf(os.Stderr, "skool: sweep saw %d members (trust floor %d); skipping vanished-reconcile to avoid mass demotion\n", len(seen), floor)
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var vanished []json.RawMessage
	for _, rec := range records {
		id := recordIdentity(rec)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		var obj map[string]json.RawMessage
		if json.Unmarshal(rec, &obj) != nil {
			continue
		}
		var state string
		_ = json.Unmarshal(obj["mbState"], &state)
		if state == stateVanished {
			continue
		}
		obj["mbState"], _ = json.Marshal(stateVanished)
		if _, ok := obj["vanished_at"]; !ok {
			obj["vanished_at"], _ = json.Marshal(now)
		}
		tagged, err := json.Marshal(obj)
		if err != nil {
			continue
		}
		vanished = append(vanished, tagged)
	}
	if len(vanished) == 0 {
		return nil
	}
	_, _, err = db.UpsertBatch("members", vanished)
	return err
}

func tagMembershipState(record json.RawMessage, state string) json.RawMessage {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(record, &obj); err != nil {
		return record
	}
	obj["mbState"], _ = json.Marshal(state)
	tagged, err := json.Marshal(obj)
	if err != nil {
		return record
	}
	return tagged
}

func readMemberRecords(db *store.Store, community string) ([]json.RawMessage, error) {
	if community == "" {
		return db.List("members", 0)
	}
	rows, err := db.Query(
		"SELECT data FROM resources WHERE resource_type='members' AND (community = ? OR community IS NULL)",
		community,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []json.RawMessage
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(data))
	}
	return out, rows.Err()
}

func decodeRoster(records []json.RawMessage) []rosterRow {
	rows := make([]rosterRow, 0, len(records))
	for _, rec := range records {
		var m rosterMember
		if err := json.Unmarshal(rec, &m); err != nil {
			continue
		}
		md := m.Member.Metadata
		state := m.MbState
		if state == "" {
			if m.Member.Churned != "" {
				state = "churned"
			} else {
				state = "active"
			}
		}
		row := rosterRow{
			ID:                 m.ID,
			Handle:             m.Name,
			Name:               strings.TrimSpace(m.FirstName + " " + m.LastName),
			Role:               m.Member.Role,
			MembershipState:    state,
			IsActive:           isActiveState(state),
			StripeSub:          md.Msbs,
			JoinSource:         md.AttrComp,
			IsTrial:            md.IsFreeTrial == 1,
			ChurnedAt:          m.Member.Churned,
			FreeTrialChurnedAt: unixToISO(md.FreeTrialChurnedAt),
			VanishedAt:         m.VanishedAt,
			RemovedAt:          jsonScalarString(m.Member.RemovedAt),
			RemovedBy:          jsonScalarString(m.Member.RemovedBy),
		}
		switch {
		case md.Mbme != "":
			row.Email, row.EmailSource = md.Mbme, "billing"
		case m.Member.InviteEmail != "":
			row.Email, row.EmailSource = m.Member.InviteEmail, "invite"
		}
		if md.Mmbp != "" {
			var price struct {
				Amount int    `json:"amount"`
				Model  string `json:"model"`
			}
			if json.Unmarshal([]byte(md.Mmbp), &price) == nil {
				row.PriceUSDMo = float64(price.Amount) / 100.0
				row.Model = price.Model
			}
		}
		if md.Mbscpe > 0 {
			row.PeriodEnd = time.Unix(md.Mbscpe, 0).UTC().Format(time.RFC3339)
		}
		if m.Member.ApprovedAt != "" {
			row.Joined = m.Member.ApprovedAt
		}
		rows = append(rows, row)
	}
	return rows
}

func filterRoster(rows []rosterRow, statuses []string, trialOnly bool, expiring int) []rosterRow {
	want := map[string]bool{}
	for _, s := range statuses {
		want[s] = true
	}
	allStates := len(want) == len(validStatuses)
	cutoff := time.Now().UTC().AddDate(0, 0, expiring)
	out := rows[:0]
	for _, r := range rows {
		if !allStates && !want[r.MembershipState] {
			continue
		}
		if trialOnly && !r.IsTrial {
			continue
		}
		if expiring > 0 {
			if r.PeriodEnd == "" {
				continue
			}
			t, err := time.Parse(time.RFC3339, r.PeriodEnd)
			if err != nil || t.After(cutoff) {
				continue
			}
		}
		out = append(out, r)
	}
	return out
}

func writeRosterCSV(cmd *cobra.Command, rows []rosterRow) error {
	w := csv.NewWriter(cmd.OutOrStdout())
	header := []string{"id", "handle", "name", "role", "membership_state", "is_active", "email", "email_source", "price_usd_mo", "billing_model", "period_end", "is_trial", "stripe_sub", "join_source", "joined"}
	if err := w.Write(header); err != nil {
		return err
	}
	for _, r := range rows {
		rec := []string{
			r.ID, r.Handle, r.Name, r.Role, r.MembershipState, strconv.FormatBool(r.IsActive),
			r.Email, r.EmailSource,
			strconv.FormatFloat(r.PriceUSDMo, 'f', -1, 64), r.Model, r.PeriodEnd,
			strconv.FormatBool(r.IsTrial), r.StripeSub, r.JoinSource, r.Joined,
		}
		if err := w.Write(rec); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func communitySuffix(community string) string {
	if community == "" {
		return ""
	}
	return " for " + community
}
