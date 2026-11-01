package business

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/aethera-deeptech/backend/internal/api"
)

const (
	uniqueViolation = "23505"
	checkViolation  = "23514"
	maxRoomRunes    = 40
	maxRooms        = 100
)

// Room is a place in the business where a device sits
type Room struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func isCheckViolation(err error) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) && pgErr.Code == checkViolation
}

// CleanName trims a name for a room or a device and checks that its length is 1 to max characters and that it has no control character
func CleanName(raw string, maxRunes int) (string, string) {
	name := strings.TrimSpace(raw)

	switch {
	case name == "":
		return "", "required"
	case utf8.RuneCountInString(name) > maxRunes:
		return "", "too_long"
	case strings.ContainsFunc(name, isHidden):
		return "", "invalid"
	default:
		return name, ""
	}
}

// Rooms lists the rooms of the business by name
func (s *Store) Rooms(ctx context.Context, businessID string) ([]Room, error) {
	rows, err := s.pool.Query(ctx, "SELECT id, name FROM rooms WHERE business_id = $1 ORDER BY lower(name), id", businessID)
	if err != nil {
		return nil, fmt.Errorf("list the rooms %w", err)
	}
	defer rows.Close()

	out := []Room{}

	for rows.Next() {
		var r Room
		if err := rows.Scan(&r.ID, &r.Name); err != nil {
			return nil, fmt.Errorf("read a room %w", err)
		}

		out = append(out, r)
	}

	return out, rows.Err()
}

// CreateRoom adds a room. A business can have at most maxRooms, so a script cannot fill the table.
func (s *Store) CreateRoom(ctx context.Context, businessID, rawName string) (Room, error) {
	name, code := CleanName(rawName, maxRoomRunes)
	if code != "" {
		return Room{}, api.Validation(map[string]string{"name": code})
	}

	var count int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM rooms WHERE business_id = $1", businessID).Scan(&count); err != nil {
		return Room{}, fmt.Errorf("count the rooms %w", err)
	}

	if count >= maxRooms {
		return Room{}, api.NewError(409, "too_many_rooms", "The business has the most rooms that it can have")
	}

	var r Room

	err := s.pool.QueryRow(ctx, "INSERT INTO rooms (business_id, name) VALUES ($1, $2) RETURNING id, name", businessID, name).Scan(&r.ID, &r.Name)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return Room{}, api.NewError(409, "room_exists", "A room with this name exists")
		}

		return Room{}, fmt.Errorf("create the room %w", err)
	}

	return r, nil
}

// isHidden reports a control character or a format character, such as a zero width space or a right to left override, which a name
// has no use for and which can make a name look like another one
func isHidden(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }
