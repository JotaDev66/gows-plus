package sqlstorage

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/avast/retry-go"
	"github.com/devlikeapro/gows/storage"
	"go.mau.fi/whatsmeow/types"
)

type SqlMessageStore struct {
	*EntityRepository[storage.StoredMessage]
}

var _ storage.MessageStorage = (*SqlMessageStore)(nil)

func (gc *GContainer) NewMessageStorage() *SqlMessageStore {
	repo := NewEntityRepository[storage.StoredMessage](
		gc.db,
		MessageTable,
		messageMapper,
	)
	return &SqlMessageStore{
		repo,
	}
}

func (s SqlMessageStore) UpsertOneMessage(msg *storage.StoredMessage) (err error) {
	return s.UpsertOne(msg)
}

func (s SqlMessageStore) GetAllMessages(filters storage.MessageFilter, sort storage.Sort, pagination storage.Pagination, merge bool) ([]*storage.StoredMessage, error) {
	conditions := make([]sq.Sqlizer, 0)
	if filters.Jid != nil {
		target := *filters.Jid
		var err error
		if merge {
			target, err = s.canonicalizeJID(*filters.Jid)
			if err != nil {
				return nil, err
			}
		}
		expr := fmt.Sprintf("%s.jid", s.table.Name)
		if merge {
			expr, err = s.primaryJIDExpression(s.table.Name)
			if err != nil {
				return nil, err
			}
		}
		conditions = append(conditions, sq.Expr(expr+" = ?", target.String()))
	}
	if filters.TimestampGte != nil {
		conditions = append(conditions, sq.GtOrEq{"timestamp": filters.TimestampGte})
	}
	if filters.TimestampLte != nil {
		conditions = append(conditions, sq.LtOrEq{"timestamp": filters.TimestampLte})
	}
	if filters.FromMe != nil {
		conditions = append(conditions, sq.Eq{"from_me": filters.FromMe})
	}
	if filters.Status != nil {
		switch s.db.DriverName() {
		case "sqlite3":
			conditions = append(conditions, sq.Expr("json_extract(data, '$.Status') = ?", *filters.Status))
		case "postgres":
			conditions = append(conditions, sq.Expr("(data::jsonb->>'Status')::int = ?", *filters.Status))
		default:
			return nil, fmt.Errorf("unsupported database driver: %s", s.db.DriverName())
		}
	}

	conditions = append(conditions, sq.Eq{"is_real": true})
	sorts := []storage.Sort{sort}
	return s.FilterBy(conditions, sorts, pagination)
}

func (s SqlMessageStore) GetChatMessages(jid types.JID, filters storage.MessageFilter, pagination storage.Pagination, merge bool) ([]*storage.StoredMessage, error) {
	filters.Jid = &jid
	sort := storage.Sort{
		Field: "timestamp",
		Order: storage.SortDesc,
	}
	return s.GetAllMessages(filters, sort, pagination, merge)
}

func (s SqlMessageStore) GetMessage(id types.MessageID) (msg *storage.StoredMessage, err error) {
	return s.GetById(id)
}

func (s SqlMessageStore) GetMessageWithRetries(id types.MessageID) (msg *storage.StoredMessage, err error) {
	err = retry.Do(
		func() error {
			msg, err = s.GetById(id)
			if err != nil {
				return err
			}
			return nil
		},
		retry.Attempts(6),
	)
	return msg, err
}

func (s SqlMessageStore) DeleteChatMessages(jid types.JID, deleteBefore time.Time) error {
	conditions := []sq.Sqlizer{
		sq.Eq{"jid": jid},
		sq.Lt{"timestamp": deleteBefore},
	}
	return s.DeleteBy(conditions)
}

func (s SqlMessageStore) DeleteMessage(id types.MessageID) error {
	return s.DeleteById(id)
}

// getLastMessagesPostgresSubquery generates the subquery for PostgreSQL to fetch the ID of the last message per chat.
func (s SqlMessageStore) getLastMessagesPostgresSubquery(primaryExpr string, priorityExpr string) *sq.SelectBuilder {
	query := sq.Select("DISTINCT ON (" + primaryExpr + ") id").
		From(s.table.Name).
		Where("is_real = true").
		OrderByClause(primaryExpr).
		OrderByClause("timestamp DESC")
	if priorityExpr != "" {
		query = query.OrderByClause(priorityExpr)
	}
	return &query
}

// getLastMessagesSQLiteSubquery generates the subquery for SQLite3 to fetch the ID of the last message per chat.
func (s SqlMessageStore) getLastMessagesSQLiteSubquery(primaryExpr string, priorityExpr string) *sq.SelectBuilder {
	ordering := "timestamp DESC"
	if priorityExpr != "" {
		ordering = fmt.Sprintf("%s, %s", ordering, priorityExpr)
	}
	query := sq.Select("id").
		FromSelect(
			sq.Select(
				"id",
				"jid",
				"timestamp",
				"ROW_NUMBER() OVER (PARTITION BY ("+primaryExpr+") ORDER BY "+ordering+") as rn",
			).
				From(s.table.Name).
				Where("is_real = true"),
			"sub").
		Where("rn = 1")
	return &query
}

// getLastMessageSubquery selects the appropriate subquery based on the database type.
func (s SqlMessageStore) getLastMessageSubquery(primaryExpr string, priorityExpr string) (*sq.SelectBuilder, error) {
	switch s.db.DriverName() {
	case "postgres":
		return s.getLastMessagesPostgresSubquery(primaryExpr, priorityExpr), nil
	case "sqlite3":
		return s.getLastMessagesSQLiteSubquery(primaryExpr, priorityExpr), nil
	default:
		return nil, fmt.Errorf("unsupported database driver: %s", s.db.DriverName())
	}
}

// GetLastMessagesInChats retrieves the last messages in chats based on filtering, sorting, and pagination.
func (s SqlMessageStore) GetLastMessagesInChats(filter storage.ChatFilter, sortBy storage.Sort, pagination storage.Pagination, merge bool) ([]*storage.StoredMessage, error) {
	primaryExpr := fmt.Sprintf("%s.jid", s.table.Name)
	var priorityExpr string
	if merge {
		var err error
		primaryExpr, err = s.primaryJIDExpression(s.table.Name)
		if err != nil {
			return nil, err
		}
		priorityExpr = s.primaryPriorityExpression(fmt.Sprintf("%s.jid", s.table.Name))
	}
	// Generate the subquery to get the ID of the last message per chat
	subQuery, err := s.getLastMessageSubquery(primaryExpr, priorityExpr)
	if err != nil {
		return nil, err
	}
	subQueryText, _, err := (*subQuery).ToSql()
	if err != nil {
		return nil, err
	}

	// Main query to get the full details of the last messages
	sql := sq.Select("data").
		From(s.table.Name).
		Where("id IN (" + subQueryText + ")")
	if len(filter.Jids) > 0 {
		targets, err := s.targetJIDStrings(filter.Jids, merge)
		if err != nil {
			return nil, err
		}
		exprColumn := primaryExpr
		if !merge {
			exprColumn = fmt.Sprintf("%s.jid", s.table.Name)
		}
		expr, args := buildInExpression(exprColumn, targets)
		sql = sql.Where(sq.Expr(expr, args...))
	}

	messages, err := s.Retrieve(sql, pagination, []storage.Sort{sortBy})
	if err != nil {
		return nil, err
	}
	if merge {
		for _, msg := range messages {
			canonical, err := s.canonicalizeJID(msg.Info.Chat)
			if err != nil {
				return nil, err
			}
			msg.Info.Chat = canonical
		}
	}

	return messages, nil
}

func (s SqlMessageStore) canonicalizeJID(jid types.JID) (types.JID, error) {
	if jid.Server != types.HiddenUserServer {
		return jid, nil
	}
	query := sq.Select("pn").
		From("whatsmeow_lid_map").
		Where(sq.Eq{"lid": jid.User}).
		Limit(1)
	sqlText, args, err := query.ToSql()
	if err != nil {
		return types.JID{}, err
	}
	var pn string
	err = s.db.Get(&pn, sqlText, args...)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return jid, nil
	case err != nil:
		return types.JID{}, err
	case pn == "":
		return jid, nil
	}
	canonical := types.JID{
		User:   pn,
		Server: types.DefaultUserServer,
		Device: jid.Device,
	}
	return canonical, nil
}

func (s SqlMessageStore) canonicalJIDStrings(jids []types.JID) ([]string, error) {
	result := make([]string, 0, len(jids))
	seen := make(map[string]struct{}, len(jids))
	for _, jid := range jids {
		canonical, err := s.canonicalizeJID(jid)
		if err != nil {
			return nil, err
		}
		str := canonical.String()
		if _, ok := seen[str]; ok {
			continue
		}
		seen[str] = struct{}{}
		result = append(result, str)
	}
	return result, nil
}

func (s SqlMessageStore) targetJIDStrings(jids []types.JID, merge bool) ([]string, error) {
	if merge {
		return s.canonicalJIDStrings(jids)
	}
	result := make([]string, 0, len(jids))
	seen := make(map[string]struct{}, len(jids))
	for _, jid := range jids {
		str := jid.String()
		if _, ok := seen[str]; ok {
			continue
		}
		seen[str] = struct{}{}
		result = append(result, str)
	}
	return result, nil
}

func (s SqlMessageStore) primaryJIDExpression(tableAlias string) (string, error) {
	column := fmt.Sprintf("%s.jid", tableAlias)
	userExpr, err := s.jidUserExpression(column)
	if err != nil {
		return "", err
	}
	pnLookup := fmt.Sprintf("(SELECT pn FROM whatsmeow_lid_map WHERE lid = %s LIMIT 1)", userExpr)
	pnJID := fmt.Sprintf("(%s || '@%s')", pnLookup, types.DefaultUserServer)
	expr := fmt.Sprintf("CASE WHEN %s LIKE '%%%%@lid' THEN COALESCE(%s, %s) ELSE %s END", column, pnJID, column, column)
	return expr, nil
}

func (s SqlMessageStore) primaryPriorityExpression(column string) string {
	return fmt.Sprintf("CASE WHEN %s LIKE '%%%%@lid' THEN 1 ELSE 0 END", column)
}

func (s SqlMessageStore) jidUserExpression(column string) (string, error) {
	switch s.db.DriverName() {
	case "postgres":
		return fmt.Sprintf("split_part(%s::text, '@', 1)", column), nil
	case "sqlite3":
		return fmt.Sprintf("substr(%s, 1, instr(%s, '@') - 1)", column, column), nil
	default:
		return "", fmt.Errorf("unsupported database driver: %s", s.db.DriverName())
	}
}

func buildInExpression(expr string, values []string) (string, []interface{}) {
	placeholders := make([]string, len(values))
	args := make([]interface{}, len(values))
	for i, value := range values {
		placeholders[i] = "?"
		args[i] = value
	}
	return fmt.Sprintf("%s IN (%s)", expr, strings.Join(placeholders, ",")), args
}
