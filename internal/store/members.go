package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// ErrAlreadyMember means the user already belongs to the board.
var ErrAlreadyMember = errors.New("already a member")

// ErrOwnerImmutable means the operation would change the owner's membership.
var ErrOwnerImmutable = errors.New("the board owner cannot be changed")

// Member is a user with a role on a board.
type Member struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	JoinedAt string `json:"joined_at"`
}

// Invite is a shareable link that grants a role on a board.
type Invite struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	ExpiresAt string `json:"expires_at"`
	CreatedAt string `json:"created_at"`
}

// MembersForBoard lists members, owners first.
func (s *Store) MembersForBoard(ctx context.Context, boardID string) ([]Member, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT m.user_id, u.username, m.role, m.created_at
		   FROM board_members m JOIN users u ON u.id = m.user_id
		  WHERE m.board_id = ?
		  ORDER BY CASE m.role WHEN 'owner' THEN 0 WHEN 'editor' THEN 1 ELSE 2 END,
		           u.username`, boardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	members := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.UserID, &m.Username, &m.Role, &m.JoinedAt); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

// MemberByUsername looks up a registered user and adds them to the board, or
// updates their role when they already belong. The owner's role is protected.
func (s *Store) MemberByUsername(ctx context.Context, boardID, username, role string) (Member, error) {
	u, _, err := s.UserByUsername(ctx, username)
	if err != nil {
		return Member{}, err
	}

	current, err := s.BoardRole(ctx, boardID, u.ID)
	switch {
	case err == nil:
		if current == RoleOwner {
			return Member{}, ErrOwnerImmutable
		}
		if _, err := s.DB.ExecContext(ctx,
			`UPDATE board_members SET role = ? WHERE board_id = ? AND user_id = ?`,
			role, boardID, u.ID); err != nil {
			return Member{}, err
		}
	case errors.Is(err, ErrNotFound):
		if _, err := s.DB.ExecContext(ctx,
			`INSERT INTO board_members (board_id, user_id, role, created_at) VALUES (?, ?, ?, ?)`,
			boardID, u.ID, role, now()); err != nil {
			if strings.Contains(err.Error(), "UNIQUE constraint failed") {
				return Member{}, ErrAlreadyMember
			}
			return Member{}, err
		}
	default:
		return Member{}, err
	}

	var joined string
	if err := s.DB.QueryRowContext(ctx,
		`SELECT created_at FROM board_members WHERE board_id = ? AND user_id = ?`,
		boardID, u.ID).Scan(&joined); err != nil {
		return Member{}, err
	}
	return Member{UserID: u.ID, Username: u.Username, Role: role, JoinedAt: joined}, nil
}

// SetMemberRole changes an existing member's role, refusing to touch the owner.
func (s *Store) SetMemberRole(ctx context.Context, boardID, userID, role string) (bool, error) {
	current, err := s.BoardRole(ctx, boardID, userID)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if current == RoleOwner {
		return false, ErrOwnerImmutable
	}
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE board_members SET role = ? WHERE board_id = ? AND user_id = ?`,
		role, boardID, userID); err != nil {
		return false, err
	}
	return true, nil
}

// RemoveMember drops a non-owner member. It reports whether a row matched.
func (s *Store) RemoveMember(ctx context.Context, boardID, userID string) (bool, error) {
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM board_members WHERE board_id = ? AND user_id = ? AND role != 'owner'`,
		boardID, userID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// newToken returns a raw URL-safe token and its storage hash.
func newToken() (raw, hash string) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(raw))
	return raw, hex.EncodeToString(sum[:])
}

// CreateInvite records an invite and returns the raw token exactly once.
func (s *Store) CreateInvite(ctx context.Context, boardID, role, createdBy string, ttl time.Duration) (string, Invite, error) {
	raw, hash := newToken()
	inv := Invite{
		ID:        randomID(),
		Role:      role,
		ExpiresAt: formatTime(time.Now().Add(ttl)),
		CreatedAt: now(),
	}
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO invites (id, board_id, token_hash, role, created_by, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		inv.ID, boardID, hash, role, createdBy, inv.ExpiresAt, inv.CreatedAt)
	if err != nil {
		return "", Invite{}, err
	}
	return raw, inv, nil
}

// InvitesForBoard lists live invites, newest first. Tokens are never returned.
func (s *Store) InvitesForBoard(ctx context.Context, boardID string) ([]Invite, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, role, expires_at, created_at FROM invites
		  WHERE board_id = ? AND expires_at > ?
		  ORDER BY created_at DESC`, boardID, now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	invites := []Invite{}
	for rows.Next() {
		var inv Invite
		if err := rows.Scan(&inv.ID, &inv.Role, &inv.ExpiresAt, &inv.CreatedAt); err != nil {
			return nil, err
		}
		invites = append(invites, inv)
	}
	return invites, rows.Err()
}

// DeleteInvite revokes an invite scoped to its board.
func (s *Store) DeleteInvite(ctx context.Context, boardID, inviteID string) (bool, error) {
	res, err := s.DB.ExecContext(ctx,
		`DELETE FROM invites WHERE id = ? AND board_id = ?`, inviteID, boardID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// InviteTarget describes what an invite grants before accepting it.
type InviteTarget struct {
	BoardID   string `json:"board_id"`
	BoardName string `json:"board_name"`
	Role      string `json:"role"`
	Valid     bool   `json:"valid"`
}

// InviteByToken resolves a raw invite token without consuming it.
func (s *Store) InviteByToken(ctx context.Context, raw string) (InviteTarget, error) {
	var t InviteTarget
	err := s.DB.QueryRowContext(ctx,
		`SELECT b.id, b.name, i.role FROM invites i JOIN boards b ON b.id = i.board_id
		  WHERE i.token_hash = ? AND i.expires_at > ?`,
		hashToken(raw), now()).Scan(&t.BoardID, &t.BoardName, &t.Role)
	if errors.Is(err, sql.ErrNoRows) {
		return InviteTarget{}, ErrNotFound
	}
	if err != nil {
		return InviteTarget{}, err
	}
	t.Valid = true
	return t, nil
}

// AcceptInvite adds the user to the invited board. Reusing a stale link when
// the user already belongs is harmless and returns their current role.
func (s *Store) AcceptInvite(ctx context.Context, raw, userID string) (InviteTarget, error) {
	target, err := s.InviteByToken(ctx, raw)
	if err != nil {
		return InviteTarget{}, err
	}

	current, err := s.BoardRole(ctx, target.BoardID, userID)
	if err == nil {
		target.Role = current
		return target, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return InviteTarget{}, err
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return InviteTarget{}, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO board_members (board_id, user_id, role, created_at) VALUES (?, ?, ?, ?)`,
		target.BoardID, userID, target.Role, now()); err != nil {
		return InviteTarget{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE invites SET accepted_at = ? WHERE token_hash = ? AND accepted_at IS NULL`,
		now(), hashToken(raw)); err != nil {
		return InviteTarget{}, err
	}
	if err := tx.Commit(); err != nil {
		return InviteTarget{}, err
	}
	return target, nil
}

// hashToken is the same SHA-256 hex digest used for session tokens.
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
