package repository

//create trigger pos_notify after insert on positions for each row execute procedure notify_trigger('uuid', 'symbol', 'open', 'close', 'is_bay');
import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx"
	_ "github.com/lib/pq"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"position-service/src/internal/data"
	"position-service/src/internal/v1"
	"position-service/src/protocol"
	"sync"
	"time"
)

type Postgres struct {
	DB            *sql.DB
	PriceChannels map[string]map[int64]chan data.SymbolPrice
	data.Positions
	data.LastPrices
}

func NewPostgres(URI string) *Postgres {
	var err error

	db := &Postgres{}
	db.DB, err = sql.Open("postgres", URI)

	log.Infoln("Initialize and connect mongo database")

	if err != nil {
		log.WithFields(log.Fields{
			"Error":    err.Error(),
			"Database": "Postgres",
		}).Error("Couldn't connect database")
		return nil
	}

	if err = db.DB.Ping(); err != nil {
		log.Error(err)
		return nil
	}

	db.LastPrices = data.LastPrices{
		Mu:     sync.Mutex{},
		Values: make(map[string]data.SymbolPrice),
	}
	db.Positions = data.Positions{
		Mu:     sync.Mutex{},
		Values: make(map[string]map[int64]data.Position),
	}
	db.PriceChannels = make(map[string]map[int64]chan data.SymbolPrice)

	if err != nil {
		log.WithFields(log.Fields{
			"Error":    err.Error(),
			"Database": "Postgres",
		}).Error("Database had not been connected")
		return nil
	}
	return db
}

func (db *Postgres) OpenPosition(ctx context.Context, price *protocol.PositionOpenReq) (*data.Position, error) {
	var pos = &data.Position{}

	openPrice := price.Ask

	if !price.IsBay {
		openPrice = price.Bid
	}

	ctx, cancel := context.WithTimeout(ctx, time.Second*500)
	defer cancel()

	if pos.UUID == 0 {
		if _, err := db.DB.ExecContext(
			ctx,
			`insert into positions (isBay,uuid, symbol, open, username) values ($1,$2,$3,$4, (select username from users where token = $5))`,
			price.IsBay,
			price.Uuid,
			price.Symbol,
			openPrice,
			price.Token); err != nil {
			log.Println(err)
			return nil, err
		}
		return pos, nil
	}
	return nil, errors.New("Position is opened ")
}

func (db *Postgres) ValidateToken(ctx context.Context, token string) bool {
	var isExist bool

	if err := db.DB.QueryRowContext(ctx, `select exists (select 1 from users where token = $1)`, token).Scan(&isExist); err != nil {
		log.Error(err)
		return isExist
	}
	return isExist
}

func (db *Postgres) IncreaseUserBalance(ctx context.Context, token string, val float32) error {
	if _, err := db.DB.ExecContext(ctx, `update users set balance = balance + $1 where token = $2`, val, token); err != nil {
		return err
	}
	return nil
}

func (db *Postgres) GetUserBalance(ctx context.Context, token string) (float32, error) {
	var balance float32

	if err := db.DB.QueryRowContext(ctx, `select balance from users where token = $1`, token).Scan(&balance); err != nil {
		return 0, err
	}
	return balance, nil

}

func (db *Postgres) GetUserPositionsByToken(ctx context.Context, token string) (map[string]data.Position, error) {
	rows, err := db.DB.QueryContext(ctx,
		`select uuid, symbol, open, isbay from positions where username = (select username from users where token = $1) and close is null`, token)

	defer func() {
		if err = rows.Close(); err != nil {
			log.Error(err)
		}
	}()

	if err != nil {
		return nil, err
	}

	positions := map[string]data.Position{}

	for rows.Next() {
		position := data.Position{}
		if err = rows.Scan(&position.UUID, &position.Symbol, &position.Open, &position.IsBay); err != nil {
			return nil, err
		}
		positions[fmt.Sprintf("%d-%s", position.UUID, position.Symbol)] = position
	}
	return positions, nil
}

func (db *Postgres) Logout(ctx context.Context, req *protocol.Token) error {
	_, err := db.DB.ExecContext(ctx, `update users set token = '' where token = $1`, req.Token)
	return err
}

func (db *Postgres) Login(ctx context.Context, req *protocol.LoginReq) (*protocol.LoginResp, error) {
	var isExist bool

	if err := db.DB.QueryRowContext(ctx, `select exists (select 1 from users where username = $1 and password = $2)`, req.Login, req.Password).Scan(&isExist); err != nil {
		return &protocol.LoginResp{Message: "FAIL"}, err
	}

	if !isExist {
		return &protocol.LoginResp{Message: "FAIL"}, errors.New("User doesnt exist ")
	}

	token, err := v1.GenerateUserToken(req.Login)

	if err != nil {
		return &protocol.LoginResp{Message: "FAIL"}, err
	}

	if _, err = db.DB.Exec(`update users set token = $1 where username = $2 and password =$3`, token, req.Login, req.Password); err != nil {
		return &protocol.LoginResp{Message: "FAIL"}, errors.New("couldn't set user token ")
	}
	return &protocol.LoginResp{
		Message: "OK",
		Token:   token,
	}, nil
}

func (db *Postgres) ClosePosition(ctx context.Context, position *protocol.PositionCloseReq) error {
	ctx, cancel := context.WithTimeout(ctx, time.Second*50)
	defer cancel()

	closePrice := position.ClosePrice

	tx, err := db.DB.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(
		ctx,
		`update positions set close = $1 where uuid = $2 and symbol = $3`,
		closePrice,
		position.Id,
		position.Symbol,
	); err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			log.Error(err)
		}
		return err
	}

	pnl := db.Positions.Values[position.Symbol][position.Id].PNL(db.LastPrices.Values[position.Symbol])

	if _, err = tx.ExecContext(
		ctx,
		`update users set balance = balance + $1 where token = $2`,
		pnl,
		position.Token,
	); err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			log.Error(err)
		}
		return err
	}

	if err = tx.Commit(); err != nil {
		log.Fatal(err)
	}
	return nil
}

func (db *Postgres) ListenNotify() error {
	_, err := pgx.NewConnPool(pgx.ConnPoolConfig{
		ConnConfig: pgx.ConnConfig{
			Host:     "localhost",
			Port:     5432,
			Database: "postgres",
			User:     "postgres",
			Password: "a!11111111",
		},
		AfterConnect: func(conn *pgx.Conn) error {
			err := conn.Listen("db_notifications")
			if err != nil {
				return err
			}

			err = conn.Listen("close_position_notify")
			if err != nil {
				return err
			}

			for {
				msg, err := conn.WaitForNotification(context.Background())
				if err != nil {
					return err
				}

				pos := data.Position{}

				if msg.Channel == "close_position_notify" {
					if err = json.Unmarshal([]byte(msg.Payload), &pos); err != nil {
						log.Errorf("error while unmarchaling: %v", err)
					}
					log.Infof("position %v-%v closed", pos.Symbol, pos.UUID)

					if ch, ok := db.PriceChannels[pos.Symbol][pos.UUID]; ok {
						close(ch)
					}
					delete(db.PriceChannels[pos.Symbol], pos.UUID)

					continue
				}

				err = json.Unmarshal([]byte(msg.Payload), &pos)
				if err != nil {
					log.Infof("%v", err)
				}
				if _, ok := db.PriceChannels[pos.Symbol]; !ok {
					db.PriceChannels[pos.Symbol] = make(map[int64]chan data.SymbolPrice)

					db.Positions.Mu.Lock()
					db.Positions.Values[pos.Symbol] = make(map[int64]data.Position)
					db.Positions.Mu.Unlock()
				}

				db.PriceChannels[pos.Symbol][pos.UUID] = make(chan data.SymbolPrice)

				db.Positions.PushValue(pos)

				if pos.UUID != 0 && pos.Symbol != "" {
					go func() {
						for {
							select {
							case lastPrice, opened := <-db.PriceChannels[pos.Symbol][pos.UUID]:
								if !opened {
									log.Infof("ret")
									return
								}
								db.LastPrices.PushValue(lastPrice)

								pnl := pos.PNL(lastPrice)
								log.Infof("pos (symbol,id){%v,%v} has pnl: %.2f", pos.Symbol, pos.UUID, pnl)
							}
						}
					}()
				}
				log.Infof("%v", pos)
			}
		},
	})
	if err != nil {
		return err
	}
	return nil
}

func (db *Postgres) ListenPosPriceUpd(msg string) error {
	conn, err := grpc.Dial("localhost:8081", grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		return err
	}

	defer func() {
		if err = conn.Close(); err != nil {
			log.Error(err)
		}

	}()

	client := protocol.NewPriceServiceClient(conn)
	res, err := client.SendPrice(context.Background(), &protocol.Conn{Message: msg})
	if err != nil {
		return err
	}
	for {
		price, err := res.Recv()
		if err != nil {
			log.Error(err)
		}

		symbolPrice := data.SymbolPrice{
			Uuid:   price.Uuid,
			Symbol: price.Symbol,
			Bid:    price.Bid,
			Ask:    price.Ask,
		}

		db.LastPrices.PushValue(symbolPrice)

		if chanMap, ok := db.PriceChannels[price.Symbol]; ok {
			for _, v := range chanMap {
				v <- symbolPrice
			}
		}
	}
}
