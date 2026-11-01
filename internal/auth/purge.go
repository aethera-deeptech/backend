package auth

import "github.com/aethera-deeptech/backend/internal/janitor"

// unverifiedGrace is how long an account may wait for its email to be verified before the account and its business are deleted
const unverifiedGrace = "7 days"

// PurgeTasks are the clean up tasks of the accounts. Each deletes a batch of rows that no rule of the service needs any more.
func PurgeTasks() []janitor.Task {
	return []janitor.Task{
		{
			Name: "sessions past their lifetime",
			Query: `DELETE FROM sessions WHERE ctid IN (
				SELECT ctid FROM sessions WHERE last_used_at < now() - interval '30 days' OR created_at < now() - interval '90 days' LIMIT $1)`,
		},
		{
			Name: "used and expired email tokens",
			Query: `DELETE FROM email_tokens WHERE ctid IN (
				SELECT ctid FROM email_tokens WHERE expires_at < now() - interval '1 day' OR used_at < now() - interval '7 days' LIMIT $1)`,
		},
		{
			Name: "rate limit events past every window",
			Query: `DELETE FROM rate_limit_events WHERE ctid IN (
				SELECT ctid FROM rate_limit_events WHERE at < now() - interval '2 hours' LIMIT $1)`,
		},
		{
			// the user goes with the business, so an account that never verified its email leaves nothing behind
			Name: "businesses that never verified an email",
			Query: `DELETE FROM businesses WHERE id IN (
				SELECT b.id FROM businesses b
				WHERE b.created_at < now() - interval '` + unverifiedGrace + `'
				  AND NOT EXISTS (SELECT 1 FROM users u WHERE u.business_id = b.id AND u.email_verified_at IS NOT NULL)
				LIMIT $1)`,
		},
	}
}
