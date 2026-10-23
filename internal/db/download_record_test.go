package db

import (
	"context"
	"errors"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/testutil"
)

// downloadRecordRow returns a sqlmock.Rows populated with a single
// download_records row in the column order ent emits on a post-update reload.
// One source of truth for the row schema keeps these specs from drifting when
// the ent schema is regenerated.
func downloadRecordRow() *sqlmock.Rows {
	GinkgoHelper()
	return sqlmock.NewRows([]string{
		"id", "create_time", "update_time", "title", "quality", "size",
		"status", "torrent_hash", "release_group", "save_path",
		"import_attempts", "failure_reason", "imported_at",
	}).AddRow(
		1, time.Now(), time.Now(), "t", "", int64(0),
		"importing", "", "", "",
		uint8(0), "", nil,
	)
}

var _ = Describe(
	"Download record store driver-error paths",
	Label("unit", "db"),
	func() {
		var (
			ctx    context.Context
			client *ent.Client
			mock   sqlmock.Sqlmock
			store  *DB
		)

		BeforeEach(func() {
			ctx = context.Background()
			client, mock = testutil.MockEntClient()
			DeferCleanup(func() { client.Close() })
			DeferCleanup(func() { Expect(mock.ExpectationsWereMet()).To(Succeed()) })
			store = New(client)
		})

		Describe("RecordImportSuccess", func() {
			When("the driver fails to begin tx", func() {
				It("returns the error", func() {
					driverErr := errors.New("begin fail")
					mock.ExpectBegin().WillReturnError(driverErr)
					err := store.RecordImportSuccess(ctx, RecordImportSuccessParams{
						RecordID: 1, MovieID: 1,
						File: MediaFileRow{Path: "/x", Size: 1},
					})
					Expect(err).To(MatchError(driverErr))
				})
			})

			When("the media file insert fails", func() {
				It("rolls back and wraps the error", func() {
					driverErr := errors.New("insert fail")
					mock.ExpectBegin()
					mock.ExpectQuery(`INSERT INTO .media_files.`).
						WillReturnError(driverErr)
					mock.ExpectRollback()
					err := store.RecordImportSuccess(ctx, RecordImportSuccessParams{
						RecordID: 1, MovieID: 1,
						File: MediaFileRow{Path: "/x", Size: 1},
					})
					Expect(err).To(MatchError(driverErr))
					Expect(err).To(MatchError(ContainSubstring("create media file")))
				})
			})

			When("the download record update fails", func() {
				It("rolls back and wraps the error", func() {
					driverErr := errors.New("update fail")
					mock.ExpectBegin()
					mock.ExpectQuery(`INSERT INTO .media_files.`).
						WillReturnRows(
							sqlmock.NewRows([]string{"id"}).AddRow(1),
						)
					mock.ExpectExec(`UPDATE .download_records.`).
						WillReturnError(driverErr)
					mock.ExpectRollback()
					err := store.RecordImportSuccess(ctx, RecordImportSuccessParams{
						RecordID: 1, MovieID: 1,
						File: MediaFileRow{Path: "/x", Size: 1},
					})
					Expect(err).To(MatchError(driverErr))
					Expect(
						err,
					).To(MatchError(ContainSubstring("update download record")))
				})
			})

			When("the movie update fails", func() {
				It("rolls back and wraps the error", func() {
					driverErr := errors.New("movie update fail")
					mock.ExpectBegin()
					mock.ExpectQuery(`INSERT INTO .media_files.`).
						WillReturnRows(
							sqlmock.NewRows([]string{"id"}).AddRow(1),
						)
					mock.ExpectExec(`UPDATE .download_records.`).
						WillReturnResult(sqlmock.NewResult(0, 1))
					mock.ExpectQuery(`SELECT .* FROM .download_records.`).
						WillReturnRows(downloadRecordRow())
					mock.ExpectExec(`UPDATE .movies.`).
						WillReturnError(driverErr)
					mock.ExpectRollback()
					err := store.RecordImportSuccess(ctx, RecordImportSuccessParams{
						RecordID: 1, MovieID: 1,
						File: MediaFileRow{Path: "/x", Size: 1},
					})
					Expect(err).To(MatchError(driverErr))
					Expect(err).To(MatchError(ContainSubstring("update movie")))
				})
			})
		})

		Describe("RecordImportFailure", func() {
			When("the driver fails to begin tx", func() {
				It("returns the error", func() {
					driverErr := errors.New("begin fail")
					mock.ExpectBegin().WillReturnError(driverErr)
					err := store.RecordImportFailure(ctx, RecordImportFailureParams{
						RecordID: 1, MovieID: 1, Terminal: true, Reason: "x",
					})
					Expect(err).To(MatchError(driverErr))
				})
			})

			When("the download record update fails", func() {
				It("rolls back and wraps the error", func() {
					driverErr := errors.New("update fail")
					mock.ExpectBegin()
					mock.ExpectExec(`UPDATE .download_records.`).
						WillReturnError(driverErr)
					mock.ExpectRollback()
					err := store.RecordImportFailure(ctx, RecordImportFailureParams{
						RecordID: 1, MovieID: 1, Terminal: true, Reason: "x",
					})
					Expect(err).To(MatchError(driverErr))
					Expect(
						err,
					).To(MatchError(ContainSubstring("update download record")))
				})
			})

			When("terminal and the movie update fails", func() {
				It("rolls back and wraps the error", func() {
					driverErr := errors.New("movie update fail")
					mock.ExpectBegin()
					mock.ExpectExec(`UPDATE .download_records.`).
						WillReturnResult(sqlmock.NewResult(0, 1))
					mock.ExpectQuery(`SELECT .* FROM .download_records.`).
						WillReturnRows(downloadRecordRow())
					mock.ExpectExec(`UPDATE .movies.`).
						WillReturnError(driverErr)
					mock.ExpectRollback()
					err := store.RecordImportFailure(ctx, RecordImportFailureParams{
						RecordID: 1, MovieID: 1, Terminal: true, Reason: "x",
					})
					Expect(err).To(MatchError(driverErr))
					Expect(err).To(MatchError(ContainSubstring("update movie")))
				})
			})
		})
	},
)
