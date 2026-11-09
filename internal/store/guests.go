package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// GuestLink is a shareable link that grants a role on a board to people who do
// not have an account.
type GuestLink struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

// GuestLinkTarget describes what a guest link grants.
type GuestLinkTarget struct {
	BoardID   string `json:"board_id"`
	BoardName string `json:"board_name"`
	Role      string `json:"role"`
	Valid     bool   `json:"valid"`
}

// CreateGuestLink records a link and returns the raw token exactly once.
func (s *Store) CreateGuestLink(ctx context.Context, boardID, role, createdBy string) (string, GuestLink, error) {
	raw, hash := newToken()
	link := GuestLink{ID: randomID(), Role: role, CreatedAt: now()}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO guest_links (id, board_id, token_hash, role, created_by, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		link.ID, boardID, hash, role, createdBy, link.CreatedAt)
	if err != nil {
		return "", GuestLink{}, err
	}
	return raw, link, nil
}

// GuestLinksForBoard lists links, newest first. Tokens are never returned.
func (s *Store) GuestLinksForBoard(ctx context.Context, boardID string) ([]GuestLink, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, role, created_at FROM guest_links
		  WHERE board_id = ? ORDER BY created_at DESC`, boardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	links := []GuestLink{}
	for rows.Next() {
		var link GuestLink
		if err := rows.Scan(&link.ID, &link.Role, &link.CreatedAt); err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	return links, rows.Err()
}

// GuestLinkByToken resolves a raw token without consuming it.
func (s *Store) GuestLinkByToken(ctx context.Context, raw string) (GuestLinkTarget, error) {
	var t GuestLinkTarget
	err := s.DB.QueryRowContext(ctx,
		`SELECT b.id, b.name, g.role
		   FROM guest_links g JOIN boards b ON b.id = g.board_id
		  WHERE g.token_hash = ?`, hashToken(raw)).
		Scan(&t.BoardID, &t.BoardName, &t.Role)
	if errors.Is(err, sql.ErrNoRows) {
		return GuestLinkTarget{}, ErrNotFound
	}
	if err != nil {
		return GuestLinkTarget{}, err
	}
	t.Valid = true
	return t, nil
}

// JoinGuestLink creates an anonymous user, adds them to the board and tracks
// the link so revoking it can end those sessions.
func (s *Store) JoinGuestLink(ctx context.Context, raw string) (GuestLinkTarget, User, error) {
	target, err := s.GuestLinkByToken(ctx, raw)
	if err != nil {
		return GuestLinkTarget{}, User{}, err
	}

	for attempt := 0; attempt < 5; attempt++ {
		u := User{
			ID:        randomID(),
			Username:  "guest-" + randomID()[:8],
			CreatedAt: now(),
		}

		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return GuestLinkTarget{}, User{}, err
		}

		_, err = tx.ExecContext(ctx,
			`INSERT INTO users (id, username, password_hash, created_at) VALUES (?, ?, '', ?)`,
			u.ID, u.Username, u.CreatedAt)
		if err != nil {
			tx.Rollback()
			if strings.Contains(err.Error(), "UNIQUE constraint failed") {
				continue
			}
			return GuestLinkTarget{}, User{}, err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO board_members (board_id, user_id, role, created_at) VALUES (?, ?, ?, ?)`,
			target.BoardID, u.ID, target.Role, u.CreatedAt); err != nil {
			tx.Rollback()
			return GuestLinkTarget{}, User{}, err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO guest_link_joins (link_id, user_id)
			 VALUES ((SELECT id FROM guest_links WHERE token_hash = ?), ?)`,
			hashToken(raw), u.ID); err != nil {
			tx.Rollback()
			return GuestLinkTarget{}, User{}, err
		}
		if err := tx.Commit(); err != nil {
			return GuestLinkTarget{}, User{}, err
		}
		return target, u, nil
	}
	return GuestLinkTarget{}, User{}, errors.New("could not allocate guest name")
}

// JoinLinkAsUser adds an existing account to the board without lowering a role
// it already has.
func (s *Store) JoinLinkAsUser(ctx context.Context, raw, userID string) (GuestLinkTarget, error) {
	target, err := s.GuestLinkByToken(ctx, raw)
	if err != nil {
		return GuestLinkTarget{}, err
	}

	current, err := s.BoardRole(ctx, target.BoardID, userID)
	switch {
	case err == nil:
		target.Role = current
		return target, nil
	case !errors.Is(err, ErrNotFound):
		return GuestLinkTarget{}, err
	}

	if _, err := s.DB.ExecContext(ctx,
		`INSERT INTO board_members (board_id, user_id, role, created_at) VALUES (?, ?, ?, ?)`,
		target.BoardID, userID, target.Role, now()); err != nil {
		return GuestLinkTarget{}, err
	}
	return target, nil
}

// DeleteGuestLink revokes a link and ends sessions created through it. Guests
// keep memberships on other boards they joined through different links.
func (s *Store) DeleteGuestLink(ctx context.Context, boardID, linkID string) (bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var found string
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM guest_links WHERE id = ? AND board_id = ?`, linkID, boardID).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM sessions WHERE user_id IN
		   (SELECT user_id FROM guest_link_joins WHERE link_id = ?)`, linkID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM board_members WHERE board_id = ? AND user_id IN
		   (SELECT user_id FROM guest_link_joins WHERE link_id = ?)`, boardID, linkID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM guest_link_joins WHERE link_id = ?`, linkID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM guest_links WHERE id = ?`, linkID); err != nil {
		return false, err
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// DeleteOrphanGuests removes anonymous accounts with no board or session left.
func (s *Store) DeleteOrphanGuests(ctx context.Context) (int64, error) {
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM users
		  WHERE username LIKE 'guest-%'
		    AND NOT EXISTS (SELECT 1 FROM board_members m WHERE m.user_id = users.id)
		    AND NOT EXISTS (SELECT 1 FROM sessions s WHERE s.user_id = users.id)`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
