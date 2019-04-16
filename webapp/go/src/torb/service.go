package main

import (
	"database/sql"
	"errors"
	"strconv"

	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo"
	"github.com/labstack/echo-contrib/session"
)

func sessUserID(c echo.Context) int64 {
	sess, _ := session.Get("session", c)
	var userID int64
	if x, ok := sess.Values["user_id"]; ok {
		userID, _ = x.(int64)
	}
	return userID
}

func sessSetUserID(c echo.Context, id int64) {
	sess, _ := session.Get("session", c)
	sess.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   3600,
		HttpOnly: true,
	}
	sess.Values["user_id"] = id
	sess.Save(c.Request(), c.Response())
}

func sessDeleteUserID(c echo.Context) {
	sess, _ := session.Get("session", c)
	sess.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   3600,
		HttpOnly: true,
	}
	delete(sess.Values, "user_id")
	sess.Save(c.Request(), c.Response())
}

func sessAdministratorID(c echo.Context) int64 {
	sess, _ := session.Get("session", c)
	var administratorID int64
	if x, ok := sess.Values["administrator_id"]; ok {
		administratorID, _ = x.(int64)
	}
	return administratorID
}

func sessSetAdministratorID(c echo.Context, id int64) {
	sess, _ := session.Get("session", c)
	sess.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   3600,
		HttpOnly: true,
	}
	sess.Values["administrator_id"] = id
	sess.Save(c.Request(), c.Response())
}

func sessDeleteAdministratorID(c echo.Context) {
	sess, _ := session.Get("session", c)
	sess.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   3600,
		HttpOnly: true,
	}
	delete(sess.Values, "administrator_id")
	sess.Save(c.Request(), c.Response())
}

func loginRequired(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, err := getLoginUser(c); err != nil {
			return resError(c, "login_required", 401)
		}
		return next(c)
	}
}

func adminLoginRequired(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, err := getLoginAdministrator(c); err != nil {
			return resError(c, "admin_login_required", 401)
		}
		return next(c)
	}
}

func getLoginUser(c echo.Context) (*User, error) {
	userID := sessUserID(c)
	if userID == 0 {
		return nil, errors.New("not logged in")
	}
	var user User
	err := db.QueryRow("SELECT id, nickname FROM users WHERE id = ?", userID).Scan(&user.ID, &user.Nickname)
	return &user, err
}

func getLoginAdministrator(c echo.Context) (*Administrator, error) {
	administratorID := sessAdministratorID(c)
	if administratorID == 0 {
		return nil, errors.New("not logged in")
	}
	var administrator Administrator
	err := db.QueryRow("SELECT id, nickname FROM administrators WHERE id = ?", administratorID).Scan(&administrator.ID, &administrator.Nickname)
	return &administrator, err
}

func getEvents(all bool) ([]*Event, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Commit()

	rows, err := tx.Query("SELECT * FROM events ORDER BY id ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*Event
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.Title, &event.PublicFg, &event.ClosedFg, &event.Price); err != nil {
			return nil, err
		}
		if !all && !event.PublicFg {
			continue
		}
		events = append(events, &event)
	}
	for i, v := range events {
		err = getEventAlreadyHavingEvent(v, -1)
		if err != nil {
			return nil, err
		}
		for k := range v.Sheets {
			v.Sheets[k].Detail = nil
		}
		events[i] = v
	}
	return events, nil
}

func getEventAlreadyHavingEvent(event *Event, loginUserID int64) error {
	event.Sheets = map[string]*Sheets{
		"S": &Sheets{},
		"A": &Sheets{},
		"B": &Sheets{},
		"C": &Sheets{},
	}

	rows, err := db.Query("SELECT * FROM sheets ORDER BY `rank`, num")
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var sheet Sheet
		if err := rows.Scan(&sheet.ID, &sheet.Rank, &sheet.Num, &sheet.Price); err != nil {
			return err
		}
		event.Sheets[sheet.Rank].Price = event.Price + sheet.Price
		event.Total++
		event.Sheets[sheet.Rank].Total++

		var reservation Reservation
		err := db.QueryRow("SELECT * FROM reservations WHERE event_id = ? AND sheet_id = ? AND canceled_at IS NULL GROUP BY event_id, sheet_id HAVING reserved_at = MIN(reserved_at)", event.ID, sheet.ID).Scan(&reservation.ID, &reservation.EventID, &reservation.SheetID, &reservation.UserID, &reservation.ReservedAt, &reservation.CanceledAt)
		if err == nil {
			sheet.Mine = reservation.UserID == loginUserID
			sheet.Reserved = true
			sheet.ReservedAtUnix = reservation.ReservedAt.Unix()
		} else if err == sql.ErrNoRows {
			event.Remains++
			event.Sheets[sheet.Rank].Remains++
		} else {
			return err
		}

		event.Sheets[sheet.Rank].Detail = append(event.Sheets[sheet.Rank].Detail, &sheet)
	}

	return nil
}

func getEvent(eventID, loginUserID int64) (*Event, error) {
	var event Event
	if err := db.QueryRow("SELECT * FROM events WHERE id = ?", eventID).Scan(&event.ID, &event.Title, &event.PublicFg, &event.ClosedFg, &event.Price); err != nil {
		return nil, err
	}
	event.Sheets = map[string]*Sheets{
		"S": &Sheets{},
		"A": &Sheets{},
		"B": &Sheets{},
		"C": &Sheets{},
	}

	event.Total = 1000
	// 各座席の予約を求める
	rows, err := db.Query("select s.rank, count(*) from reservations inner join sheets s on s.id = sheet_id where event_id = ? and canceled_at is null group by s.rank", event.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var reserved_total int
	for rows.Next() {
		var rank string
		var count int
		if err = rows.Scan(&rank, &count); err != nil {
			return nil, err
		}
		event.Sheets[rank].Remains = count
		reserved_total = reserved_total + count
	}

	arr := []string{"S", "A", "B", "C"}
	for _, v := range arr {
		sheet, i := getSheetsInfo(v, "1")
		if i < 0 {
			return nil, errors.New("non range")
		}
		event.Sheets[v].Price = sheet.Price + event.Price
		event.Sheets[v].Total = sheet.Total
		event.Sheets[v].Remains = sheet.Total - reserved_total
	}
	event.Remains = 1000 - reserved_total

	// reserved sheetだけを取る
	rows, err = db.Query("select sheet_id, user_id, reserved_at from reservations where event_id = ?", event.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	reservedSheets := map[int64]TempStruct{}
	for rows.Next() {
		var temp TempStruct
		rows.Scan(&temp.SheetID, &temp.UserID, &temp.ReservedAt)
		reservedSheets[temp.SheetID] = temp
	}
	var i int64
	for i = 1; i <= 1000; i++ {
		if _, ok := reservedSheets[i]; ok {
			sheet, e := getSheetByID(i)
			if e < 0 {
				return nil, errors.New("non range")
			}
			if reservedSheets[i].UserID == loginUserID {
				sheet.Mine = true
			}
			sheet.ReservedAt = reservedSheets[i].ReservedAt
			sheet.Reserved = true
			event.Sheets[sheet.Rank].Detail = append(event.Sheets[sheet.Rank].Detail, &sheet)
		} else {
			sheet, e := getSheetByID(i)
			if e < 0 {
				return nil, errors.New("non range")
			}
			sheet.Reserved = false
			event.Sheets[sheet.Rank].Detail = append(event.Sheets[sheet.Rank].Detail, &sheet)
		}
	}

	return &event, nil
}

func sanitizeEvent(e *Event) *Event {
	sanitized := *e
	sanitized.Price = 0
	sanitized.PublicFg = false
	sanitized.ClosedFg = false
	return &sanitized
}

func fillinUser(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if user, err := getLoginUser(c); err == nil {
			c.Set("user", user)
		}
		return next(c)
	}
}

func fillinAdministrator(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if administrator, err := getLoginAdministrator(c); err == nil {
			c.Set("administrator", administrator)
		}
		return next(c)
	}
}

func validateRank(rank string) bool {
	switch rank {
	case "A", "B", "C", "S":
		return true
	default:
		return false
	}
}

func getIdByRankAndNum(rank string, id string) int {
	num, err := strconv.Atoi(id)
	if err != nil {
		return -1
	}
	switch rank {
	case "S":
		if 0 < num && num <= 50 {
			return num
		}
	case "A":
		if 0 < num && num <= 150 {
			return num + 50
		}
	case "B":
		if 0 < num && num <= 300 {
			return num + 200
		}
	case "C":
		if 0 < num && num <= 500 {
			return num + 500
		}
	}
	return -1
}

func getSheetsInfo(rank, num_s string) (Sheet, int) {
	num, err := strconv.ParseInt(num_s, 10, 64)
	var sheet Sheet
	if err != nil {
		return sheet, -1
	}
	switch rank {
	case "S":
		if 0 < num && num <= 50 {
			sheet.ID = num
			sheet.Rank = "S"
			sheet.Price = 5000
			sheet.Num = num
			sheet.Total = 50
			return sheet, 1
		}
	case "A":
		if 0 < num && num <= 150 {
			sheet.ID = num + 50
			sheet.Rank = "A"
			sheet.Price = 3000
			sheet.Num = num
			sheet.Total = 150
			return sheet, 1
		}
	case "B":
		if 0 < num && num <= 300 {
			sheet.ID = num + 200
			sheet.Rank = "B"
			sheet.Price = 1000
			sheet.Num = num
			sheet.Total = 300
			return sheet, 1
		}
	case "C":
		if 0 < num && num <= 500 {
			sheet.ID = num + 500
			sheet.Rank = "C"
			sheet.Price = 0
			sheet.Num = num
			sheet.Total = 500
			return sheet, 1
		}
	}
	return sheet, -1
}
func getSheetByID(id int64) (Sheet, int8) {
	var sheet Sheet
	if 1 <= id && id <= 50 {
		sheet.ID = id
		sheet.Rank = "S"
		sheet.Price = 5000
		sheet.Num = id
		sheet.Total = 50
		return sheet, 1
	} else if 51 <= id && id <= 200 {
		sheet.ID = id
		sheet.Rank = "A"
		sheet.Price = 3000
		sheet.Num = id - 50
		sheet.Total = 150
		return sheet, 1
	} else if 201 <= id && id <= 500 {
		sheet.ID = id
		sheet.Rank = "B"
		sheet.Price = 1000
		sheet.Num = id - 200
		sheet.Total = 300
		return sheet, 1
	} else if 501 <= id && id <= 1000 {
		sheet.ID = id
		sheet.Rank = "C"
		sheet.Price = 0
		sheet.Num = id - 500
		sheet.Total = 500
		return sheet, 1
	}
	return sheet, -1
}
