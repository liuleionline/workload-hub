package main

// seedOfficialCalendar installs the annual working-day baseline published by
// the General Office of the State Council. INSERT OR IGNORE preserves every
// later adjustment made by a system administrator.
func (db *DB) seedOfficialCalendar() error {
	holidays := map[string]string{
		"2026-01-01": "元旦", "2026-01-02": "元旦", "2026-01-03": "元旦",
		"2026-02-15": "春节", "2026-02-16": "春节", "2026-02-17": "春节", "2026-02-18": "春节", "2026-02-19": "春节", "2026-02-20": "春节", "2026-02-21": "春节", "2026-02-22": "春节", "2026-02-23": "春节",
		"2026-04-04": "清明节", "2026-04-05": "清明节", "2026-04-06": "清明节",
		"2026-05-01": "劳动节", "2026-05-02": "劳动节", "2026-05-03": "劳动节", "2026-05-04": "劳动节", "2026-05-05": "劳动节",
		"2026-06-19": "端午节", "2026-06-20": "端午节", "2026-06-21": "端午节",
		"2026-09-25": "中秋节", "2026-09-26": "中秋节", "2026-09-27": "中秋节",
		"2026-10-01": "国庆节", "2026-10-02": "国庆节", "2026-10-03": "国庆节", "2026-10-04": "国庆节", "2026-10-05": "国庆节", "2026-10-06": "国庆节", "2026-10-07": "国庆节",
	}
	workdays := map[string]string{
		"2026-01-04": "元旦调休上班", "2026-02-14": "春节调休上班", "2026-02-28": "春节调休上班",
		"2026-05-09": "劳动节调休上班", "2026-09-20": "国庆节调休上班", "2026-10-10": "国庆节调休上班",
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for date, label := range holidays {
		if _, err = tx.Exec(`INSERT OR IGNORE INTO work_calendar(work_date,work_hours,label,source) VALUES (?,0,?,'国务院办公厅2026')`, date, label); err != nil {
			return err
		}
	}
	for date, label := range workdays {
		if _, err = tx.Exec(`INSERT OR IGNORE INTO work_calendar(work_date,work_hours,label,source) VALUES (?,8,?,'国务院办公厅2026')`, date, label); err != nil {
			return err
		}
	}
	return tx.Commit()
}
