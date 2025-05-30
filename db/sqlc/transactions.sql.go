// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.29.0
// source: transactions.sql

package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

const addCoins = `-- name: AddCoins :exec
UPDATE users SET in_hand=in_hand + $3 WHERE id = $1 AND guild_id = $2
`

type AddCoinsParams struct {
	ID      int64       `json:"id"`
	GuildID int64       `json:"guildId"`
	InHand  pgtype.Int8 `json:"inHand"`
}

func (q *Queries) AddCoins(ctx context.Context, arg AddCoinsParams) error {
	_, err := q.db.Exec(ctx, addCoins, arg.ID, arg.GuildID, arg.InHand)
	return err
}

const depositAmount = `-- name: DepositAmount :exec
UPDATE users SET in_hand = in_hand - $3, garrix_coins = garrix_coins + $3 WHERE id = $1 AND guild_id = $2
`

type DepositAmountParams struct {
	ID      int64       `json:"id"`
	GuildID int64       `json:"guildId"`
	InHand  pgtype.Int8 `json:"inHand"`
}

func (q *Queries) DepositAmount(ctx context.Context, arg DepositAmountParams) error {
	_, err := q.db.Exec(ctx, depositAmount, arg.ID, arg.GuildID, arg.InHand)
	return err
}

const getBalance = `-- name: GetBalance :one
SELECT garrix_coins, in_hand FROM users WHERE id = $1 AND guild_id = $2
`

type GetBalanceParams struct {
	ID      int64 `json:"id"`
	GuildID int64 `json:"guildId"`
}

type GetBalanceRow struct {
	GarrixCoins pgtype.Int8 `json:"garrixCoins"`
	InHand      pgtype.Int8 `json:"inHand"`
}

func (q *Queries) GetBalance(ctx context.Context, arg GetBalanceParams) (GetBalanceRow, error) {
	row := q.db.QueryRow(ctx, getBalance, arg.ID, arg.GuildID)
	var i GetBalanceRow
	err := row.Scan(&i.GarrixCoins, &i.InHand)
	return i, err
}

const giveCoins = `-- name: GiveCoins :exec
WITH sender_update AS (
    UPDATE users AS sender
    SET in_hand = sender.in_hand - $4
    WHERE sender.id = $1 AND sender.in_hand >= $3 AND sender.guild_id = $3
    RETURNING sender.id
)
UPDATE users AS receiver
SET in_hand = receiver.in_hand + $4
WHERE receiver.id = $2 AND receiver.guild_id = $3
AND EXISTS (SELECT 1 FROM sender_update)
`

type GiveCoinsParams struct {
	ID      int64       `json:"id"`
	ID_2    int64       `json:"id2"`
	GuildID int64       `json:"guildId"`
	InHand  pgtype.Int8 `json:"inHand"`
}

func (q *Queries) GiveCoins(ctx context.Context, arg GiveCoinsParams) error {
	_, err := q.db.Exec(ctx, giveCoins,
		arg.ID,
		arg.ID_2,
		arg.GuildID,
		arg.InHand,
	)
	return err
}

const withdrawAmount = `-- name: WithdrawAmount :exec
UPDATE users SET in_hand = in_hand + $3, garrix_coins = garrix_coins - $3 WHERE id = $1 AND guild_id = $2
`

type WithdrawAmountParams struct {
	ID      int64       `json:"id"`
	GuildID int64       `json:"guildId"`
	InHand  pgtype.Int8 `json:"inHand"`
}

func (q *Queries) WithdrawAmount(ctx context.Context, arg WithdrawAmountParams) error {
	_, err := q.db.Exec(ctx, withdrawAmount, arg.ID, arg.GuildID, arg.InHand)
	return err
}
