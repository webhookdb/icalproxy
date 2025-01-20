package pgxt_test

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lithictech/go-aperitif/v2/logctx"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/webhookdb/icalproxy/appglobals"
	"github.com/webhookdb/icalproxy/config"
	"github.com/webhookdb/icalproxy/pgxt"
	"testing"
)

func TestPGXT(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "pgxt Suite")
}

var _ = Describe("pgxt", func() {
	var ctx context.Context
	var cfg config.Config
	var ag *appglobals.AppGlobals

	BeforeEach(func() {
		var err error
		ctx, _ = logctx.WithNullLogger(context.Background())
		cfg, err = config.LoadConfig()
		Expect(err).ToNot(HaveOccurred())
		ag, err = appglobals.New(ctx, cfg)
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("scalar select", func() {
		It("can select a scalar value", func() {
			val, err := pgxt.GetScalar[int](ctx, ag.DB, "SELECT 123")
			Expect(err).ToNot(HaveOccurred())
			Expect(val).To(Equal(123))
		})
		It("can select a scalar value", func() {
			vals, err := pgxt.GetScalars[int](ctx, ag.DB, "SELECT 123 FROM generate_series(0,1)")
			Expect(err).ToNot(HaveOccurred())
			Expect(vals).To(Equal([]int{123, 123}))
		})
	})

	Describe("transactions", func() {
		It("can commit the transaction on success", func() {
			simpleExec := func(p *pgxpool.Config) {
				p.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
			}
			c1, _ := pgxt.ConnectToUrl(cfg.DatabaseUrl, simpleExec)
			c2, _ := pgxt.ConnectToUrl(cfg.DatabaseUrl, simpleExec)
			defer c1.Close()
			defer c2.Close()
			c1.Exec(ctx, "DROP TABLE IF EXISTS transactiontest")
			c1.Exec(ctx, "CREATE TABLE transactiontest(id integer)")
			Expect(pgxt.WithTransaction(ctx, c1, func(t pgx.Tx) error {
				_, err := t.Exec(ctx, "INSERT INTO transactiontest VALUES (1)")
				Expect(err).ToNot(HaveOccurred())
				return nil
			})).To(Succeed())
			count, err := pgxt.GetScalar[int](ctx, c2, "SELECT count(1) from transactiontest")
			Expect(err).ToNot(HaveOccurred())
			Expect(count).To(Equal(1))

			sentinel := errors.New("sentinel")
			Expect(pgxt.WithTransaction(ctx, c1, func(t pgx.Tx) error {
				_, err := t.Exec(ctx, "INSERT INTO transactiontest VALUES (2)")
				Expect(err).ToNot(HaveOccurred())
				return sentinel
			})).To(Equal(sentinel))
			count, err = pgxt.GetScalar[int](ctx, c2, "SELECT count(1) from transactiontest")
			Expect(err).ToNot(HaveOccurred())
			Expect(count).To(Equal(1))

			Expect(func() {
				pgxt.WithTransaction(ctx, c1, func(t pgx.Tx) error {
					_, err := t.Exec(ctx, "INSERT INTO transactiontest VALUES (2)")
					Expect(err).ToNot(HaveOccurred())
					panic(sentinel)
				})
			}).To(PanicWith(sentinel))
			count, err = pgxt.GetScalar[int](ctx, c2, "SELECT count(1) from transactiontest")
			Expect(err).ToNot(HaveOccurred())
			Expect(count).To(Equal(1))
		})

		It("returns the original transaction if nested", func() {
			conn, _ := pgxt.ConnectToUrl(cfg.DatabaseUrl)
			defer conn.Close()
			Expect(pgxt.WithTransaction(ctx, conn, func(t1 pgx.Tx) error {
				return pgxt.WithTransaction(ctx, t1, func(t2 pgx.Tx) error {
					Expect(t2).To(BeIdenticalTo(t1))
					return nil
				})
			})).To(Succeed())
		})

		It("ignores tx closed errors on rollback or commit", func() {
			conn, _ := pgxt.ConnectToUrl(cfg.DatabaseUrl)
			defer conn.Close()
			Expect(pgxt.WithTransaction(ctx, conn, func(t1 pgx.Tx) error {
				Expect(t1.Commit(ctx)).To(Succeed())
				return nil
			})).To(Succeed())

			Expect(pgxt.WithTransaction(ctx, conn, func(t1 pgx.Tx) error {
				Expect(t1.Rollback(ctx)).To(Succeed())
				return nil
			})).To(Succeed())
		})
	})
})
