package relay

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/aethera-deeptech/backend/internal/janitor"
)

// Querier is what the gateway code gives to the relay, a pool or a transaction
type Querier = querier

// Fetch returns the commands that wait for a gateway and marks the new ones as sent. A command that was sent already is returned
// again until the gateway reports a result for it, because the answer that carried it may have been lost.
func Fetch(ctx context.Context, q Querier, gatewayID string) ([]Command, error) {
	rows, err := q.Query(ctx,
		"SELECT id, node_id, action, expires_at, status FROM relay_commands WHERE gateway_id = $1 AND status IN ('pending', 'sent') AND expires_at > now() ORDER BY created_at, id", gatewayID)
	if err != nil {
		return nil, fmt.Errorf("read the commands %w", err)
	}
	defer rows.Close()

	out := []Command{}
	fresh := []string{}

	for rows.Next() {
		var (
			c      Command
			status string
		)

		if err := rows.Scan(&c.ID, &c.NodeID, &c.Action, &c.ExpiresAt, &status); err != nil {
			return nil, fmt.Errorf("read a command %w", err)
		}

		out = append(out, c)

		if status == "pending" {
			fresh = append(fresh, c.ID)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the commands %w", err)
	}

	rows.Close()

	if len(fresh) > 0 {
		if _, err := q.Exec(ctx, "UPDATE relay_commands SET status = 'sent', sent_at = now() WHERE id = ANY($1::uuid[]) AND status = 'pending'", fresh); err != nil {
			return nil, fmt.Errorf("mark the commands as sent %w", err)
		}
	}

	return out, nil
}

// Complete stores the results that a gateway reports. A result for a command of another gateway, one that is not a number of a
// command, or one that already ended is ignored, and the number of ignored results is returned.
func Complete(ctx context.Context, q Querier, gatewayID string, results []Result) (int, error) {
	ignored := 0

	for i, r := range results {
		if i >= MaxResults {
			ignored += len(results) - i

			break
		}

		// the parsed value goes to the query, because uuid.Parse also accepts the urn:uuid: form that PostgreSQL does not, and a query
		// that fails would fail the whole batch for ever
		id, err := uuid.Parse(r.ID)
		if err != nil {
			ignored++

			continue
		}

		tag, err := q.Exec(ctx,
			"UPDATE relay_commands SET status = CASE WHEN $3 THEN 'applied' ELSE 'failed' END, done_at = now() WHERE id = $1 AND gateway_id = $2 AND status IN ('pending', 'sent')",
			id.String(), gatewayID, r.OK)
		if err != nil {
			return ignored, fmt.Errorf("store a result %w", err)
		}

		if tag.RowsAffected() == 0 {
			ignored++

			continue
		}

		// a switch that worked moves the state of the node at once, unless a sample of the same batch is newer, and a sample that the plug
		// sends later moves it again
		if r.OK {
			if _, err := q.Exec(ctx,
				"UPDATE nodes n SET relay_on = (c.action = 'on'), relay_at = now() FROM relay_commands c WHERE c.id = $1 AND n.gateway_id = c.gateway_id AND n.node_id = c.node_id AND (n.relay_at IS NULL OR n.relay_at < now())",
				id.String()); err != nil {
				return ignored, fmt.Errorf("move the state of the relay %w", err)
			}
		}
	}

	return ignored, nil
}

// Observe stores the state of the relay that a plug reported in a sample. An older sample never replaces a newer state.
func Observe(ctx context.Context, q Querier, gatewayID, nodeID string, on bool, at time.Time) error {
	if _, err := q.Exec(ctx,
		"UPDATE nodes SET relay_on = $3, relay_at = $4 WHERE gateway_id = $1 AND node_id = $2 AND (relay_at IS NULL OR relay_at < $4)",
		gatewayID, nodeID, on, at); err != nil {
		return fmt.Errorf("store the state of the relay %w", err)
	}

	return nil
}

// PurgeTasks are the clean up tasks of the relay
func PurgeTasks() []janitor.Task {
	return []janitor.Task{
		{
			Name:  "relay commands older than 30 days",
			Query: `DELETE FROM relay_commands WHERE ctid IN (SELECT ctid FROM relay_commands WHERE created_at < now() - interval '30 days' LIMIT $1)`,
		},
	}
}
