package sqlite

import (
	"database/sql"
	"fmt"
	"github.com/Percona-Lab/go-tpcc/tpcc/models"
	"github.com/ardhipoetra/go-dqlite/client"
	"github.com/ardhipoetra/go-dqlite/driver"
	"github.com/ardhipoetra/go-dqlite/protocol"
	_ "github.com/mattn/go-sqlite3"
	"reflect"
	"strconv"
	"strings"
	"time"
	"context"
)

type SQLite struct {
	transactions bool
	Client *sql.DB
	fk bool
	preparedStatements bool
	tx *sql.Tx
	isTx bool
}

var leader_cli *client.Client
var voter_cli []*client.Client

var leadercli *client.Client

func connectClients(leaderu string, voteru []string) {
	// connect each client with dqlite instance
	leader_cli, _ = client.New(context.Background(), leaderu)
	for i, v := range voteru {
		fmt.Printf("Voter %d %v\n", i, v)
		voter_cli[i], _ = client.New(context.Background(), v)
	}
}

func setupCluster(leaderu string, voteru []string) protocol.NodeStore {
	// set the inmemnodestore to refer the cluster
	store := client.NewInmemNodeStore()
	store.Set(context.Background(), []client.NodeInfo{{Address: leaderu}})

	fmt.Printf("Find leader...")
	leadercli, _ := client.FindLeader(context.Background(), store, []client.Option{client.WithDialFunc(client.DefaultDialFunc)}...)
	fmt.Printf("Leader is: %s", leadercli)

	for i, v := range voteru {
		// prepare node 2 and 3 to be added to the leader
		// the leader by default has ID = 1 or BootstrapID (some hardcoded value)
		client_voter := client.NodeInfo{ID: uint64(i)+uint64(2), Address: v, Role: client.Voter}

		// add node2
		fmt.Printf("(%d) Add Client %s ...", client_voter.ID, client_voter)
		err := leadercli.Add(context.Background(), client_voter)
		if err != nil {
			fmt.Errorf("Cannot add node %s %s\n", client_voter, err)
		}
	}
	return store
}



func NewSqlite(uri string, dbname string, transactions bool,
	leaderu string, voteru []string) (*SQLite, error) {

	voter_cli = make([]*client.Client, len(voteru))
	connectClients(leaderu, voteru)
	store := setupCluster(leaderu, voteru)
	driver, _ := driver.New(store)

	dvs := sql.Drivers()
	found := false
	for _, a := range dvs {
        if a == "dqlite" {
            found = true
        }
    }

	if !found {
		sql.Register("dqlite", driver)
	}

	db, err := sql.Open("dqlite", "dqlite_tpcc")
	db.SetMaxOpenConns(1)

	if err != nil {
		return nil, err
	}

	printCluster()

	return &SQLite{
		transactions: transactions,
		Client: db,
		fk: true,
		preparedStatements: false,
	}, nil
}

func (db *SQLite) StartTrx() error {
	tx, err := db.Client.Begin()
	if err != nil {
		return err
	}

	db.tx = tx
	return nil
}

func (db *SQLite) CommitTrx() error {
	return db.tx.Commit()
}

func (db *SQLite) RollbackTrx() error {
	return db.tx.Rollback()
}

func (db *SQLite) transformQuery(query string, args ...interface{}) (string, []interface{}) {

	for k,v := range args {
		switch v.(type) {
		case time.Time:
			args[k] = v.(time.Time).Format("2006-01-01 15:04:05")
		}
	}


	if ! db.preparedStatements {
		query = fmt.Sprintf(
			strings.Replace(query, "?", "'%v'", -1),
			args...
		)

		args = nil
	}

	return query, args
}

func (db *SQLite) query(query string, args ...interface{}) (*sql.Rows, error){

	query, args = db.transformQuery(query,args...)

	if db.transactions && db.isTx {
		return db.tx.Query(query, args...)
	}

	return db.Client.Query(query, args...)
}

func (db *SQLite) queryRow(query string, args ...interface{}) *sql.Row {

	query, args = db.transformQuery(query,args...)

	if db.transactions && db.isTx {
		return db.tx.QueryRow(query, args...)
	}

	return db.Client.QueryRow(query, args...)
}
func (db *SQLite) exec(query string, args ...interface{}) (sql.Result, error){

	query, args = db.transformQuery(query,args...)

	if db.transactions && db.isTx {
		return db.tx.Exec(query, args...)
	}

	return db.Client.Exec(query, args...)
}

func (db *SQLite) InsertOne(tableName string, d interface{}) error {
	v := reflect.ValueOf(d)
	t := v.Type()
	var fields []string
	var values []interface{}

	for i := 0; i < v.NumField(); i++ {
		//TODO: it should probably be a bit more better designed
		if _, ok := t.Field(i).Tag.Lookup("sql"); ok {
			continue
		}

		fields = append(fields, t.Field(i).Name)
		values = append(values, v.Field(i).Interface())
	}

	f := strings.Join(fields, ",")

	if db.preparedStatements {

		query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", tableName, f, strings.Repeat(",?", len(fields))[1:])
		_, err := db.Client.Exec(query, values...)
		return err
	}

	//If you don’t want to use a prepared statement, you need to use fmt.Sprint() or similar to assemble the SQL,
	//and pass this as the only argument to db.Query() or db.QueryRow().
	//
	//http://go-database-sql.org/prepared.html

	var values_ []string
	for _, v := range values {
		switch v.(type) {
		case time.Time:
			values_ = append(values_, fmt.Sprintf("'%v'", v.(time.Time).Format("2006-01-01 15:04:05")))
		case string:
			values_ = append(values_, fmt.Sprintf("'%s'", v))
		default:
			values_ = append(values_, fmt.Sprintf("%v", v))
		}
	}

	_,err := db.Client.Exec(
		fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", tableName, f, strings.Join(values_, ",")),
	)
	if err != nil {
		panic(err)
	}

	return err
}

func (db *SQLite) InsertBatch(tableName string, d []interface{}) error {
	for _, item := range d {
		err := db.InsertOne(tableName, item)
		if err != nil {
			return err
		}
	}

	return nil
}

func (db *SQLite) IncrementDistrictOrderId(warehouseId int, districtId int) error {
	query := "UPDATE DISTRICT SET D_NEXT_O_ID = D_NEXT_O_ID+? WHERE D_ID = ? AND D_W_ID = ?"

	r, err := db.exec(query, 1, districtId, warehouseId)

	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()

	if n == 0 {
		return fmt.Errorf("unable to match district")
	}

	return nil
}

func (db *SQLite) GetNewOrder(warehouseId int, districtId int) (*models.NewOrder, error) {

	var query string
	if db.transactions {
		query = "SELECT NO_O_ID FROM NEW_ORDERS WHERE NO_D_ID = ? AND NO_W_ID = ? ORDER BY NO_O_ID ASC LIMIT 1 FOR UPDATE"
	} else {
		query = "SELECT NO_O_ID FROM NEW_ORDERS WHERE NO_D_ID = ? AND NO_W_ID = ? ORDER BY NO_O_ID ASC LIMIT 1"
	}
	r := db.queryRow(query, districtId, warehouseId)

	var no models.NewOrder
	err := r.Scan(&no.NO_O_ID)

	if err != nil {
		return nil, err
	}

	return &no, nil
}

func (db *SQLite) DeleteNewOrder(orderId int, warehouseId int, districtId int) error {

	query := "DELETE FROM NEW_ORDERS WHERE NO_O_ID = ? AND NO_D_ID = ? AND NO_W_ID = ?"
	r, err := db.exec(query, orderId, districtId, warehouseId)

	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return fmt.Errorf("unable to match new order for delete")
	}

	return nil
}

func (db *SQLite) GetCustomer(customerId int, warehouseId int, districtId int) (*models.Customer, error) {

	query := "SELECT C_ID, C_D_ID, C_W_ID, C_FIRST, C_MIDDLE, C_LAST, C_STREET_1, C_STREET_2, C_CITY, C_STATE, C_ZIP, " +
		"C_PHONE, C_SINCE, C_CREDIT, C_CREDIT_LIM :: float, C_DISCOUNT, C_BALANCE, C_YTD_PAYMENT, C_PAYMENT_CNT, C_DELIVERY_CNT, C_DATA " +
		"FROM CUSTOMER WHERE C_W_ID = ? AND C_D_ID = ? AND C_ID = ?"

	var customer models.Customer

	r := db.queryRow(query, warehouseId, districtId, customerId)

	err := r.Scan(
		&customer.C_ID,
		&customer.C_D_ID,
		&customer.C_W_ID,
		&customer.C_FIRST,
		&customer.C_MIDDLE,
		&customer.C_LAST,
		&customer.C_STREET_1,
		&customer.C_STREET_2,
		&customer.C_CITY,
		&customer.C_STATE,
		&customer.C_ZIP,
		&customer.C_PHONE,
		&customer.C_SINCE,
		&customer.C_CREDIT,
		&customer.C_CREDIT_LIM,
		&customer.C_DISCOUNT,
		&customer.C_BALANCE,
		&customer.C_YTD_PAYMENT,
		&customer.C_PAYMENT_CNT,
		&customer.C_DELIVERY_CNT,
		&customer.C_DATA,
	)

	if err != nil {
		return nil, err
	}

	return &customer, nil
}

func (db *SQLite) GetCustomerIdOrder(orderId int, warehouseId int, districtId int) (int, error) {
	query := "SELECT O_C_ID FROM ORDERS WHERE O_ID = ? AND O_D_ID = ? AND O_W_ID = ?"

	r := db.queryRow(query, orderId, districtId, warehouseId)

	var cId int

	err := r.Scan(&cId)

	if err != nil {
		return 0, err
	}

	return cId, nil
}

func (db *SQLite) UpdateOrders(orderId int, warehouseId int, districtId int, oCarrierId int, deliveryDate time.Time) error {
	query := "UPDATE ORDERS SET O_CARRIER_ID = ? WHERE O_ID = ? AND O_D_ID = ? AND O_W_ID = ?"
	r, err := db.exec(query, oCarrierId, orderId, districtId, warehouseId)
	if err != nil {
		return err
	}

	if err != nil {
		return err
	}

	n, _ := r.RowsAffected()
	if n == 0 {
		fmt.Errorf("unable to match customer")
	}

	query = "UPDATE ORDER_LINE SET OL_DELIVERY_D = ? WHERE OL_O_ID = ? AND OL_D_ID = ? AND OL_W_ID = ?"
	r, err = db.exec(query, deliveryDate, orderId, districtId, warehouseId)
	if err != nil {
		return err
	}

	return nil
}

func (db *SQLite) SumOLAmount(orderId int, warehouseId int, districtId int) (float64, error) {
	query := "SELECT SUM(ol_amount) FROM ORDER_LINE WHERE OL_O_ID = ? AND OL_D_ID = ? AND OL_W_ID = ?"
	row := db.queryRow(query, orderId, districtId, warehouseId)
	var sum float64
	err := row.Scan(&sum)
	if err != nil {
		return 0, err
	}

	return sum, nil
}

func (db *SQLite) UpdateCustomer(customerId int, warehouseId int, districtId int, sumOlTotal float64) error {
	query := "UPDATE CUSTOMER SET C_BALANCE = C_BALANCE + ? WHERE C_ID = ? AND C_D_ID = ? AND C_W_ID = ?"

	res, err := db.exec(query, sumOlTotal, customerId, districtId, warehouseId)
	if err != nil {
		return err
	}

	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("unable to match customer")
	}

	return nil
}

func (db *SQLite) GetNextOrderId(warehouseId int, districtId int) (int, error) {
	query := "SELECT D_NEXT_O_ID FROM DISTRICT WHERE D_ID = ? AND D_W_ID = ?"

	row := db.queryRow(query, districtId, warehouseId)
	var dn int
	err := row.Scan(&dn)
	if err != nil {
		return 0, err
	}

	return dn, nil
}

func (db *SQLite) GetStockCount(orderIdLt int, orderIdGt int, threshold int, warehouseId int, districtId int) (int64, error) {
	query := "SELECT COUNT(DISTINCT(OL_I_ID)) FROM " +
		"ORDER_LINE, STOCK " +
		"WHERE " +
		"OL_W_ID = ? AND OL_D_ID = ? " +
		"AND OL_O_ID < ? AND OL_O_ID >= ? " +
		"AND S_W_ID = ? AND S_I_ID = OL_I_ID AND S_QUANTITY < ?"


	row := db.queryRow(query, warehouseId, districtId, orderIdLt, orderIdGt, warehouseId, threshold)
	var count int64
	err := row.Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

func (db *SQLite) GetCustomerById(customerId int, warehouseId int, districtId int) (*models.Customer, error) {
	var c models.Customer

	query := "SELECT C_ID, C_FIRST, C_MIDDLE, C_LAST, C_BALANCE FROM CUSTOMER WHERE C_ID = ? AND C_W_ID = ? and C_D_ID = ?"

	row := db.queryRow(query, customerId, warehouseId, districtId)
	err := row.Scan(&c.C_ID, &c.C_FIRST, &c.C_MIDDLE, &c.C_LAST, &c.C_BALANCE)
	if err != nil {
		return nil, err
	}

	return &c, nil;
}

func (db *SQLite) GetCustomerByName(name string, warehouseId int, districtId int) (*models.Customer, error) {
	query := "SELECT C_ID, C_FIRST, C_MIDDLE, C_LAST, C_BALANCE FROM CUSTOMER WHERE C_W_ID = ? AND C_D_ID = ? AND C_LAST = ?"

	rows,err := db.query(query, warehouseId, districtId, name)
	defer rows.Close()
	if err != nil {
		return nil, err
	}
	var customer models.Customer
	var customers []models.Customer
	for rows.Next() {
		err = rows.Scan(
			&customer.C_ID,
			&customer.C_FIRST,
			&customer.C_MIDDLE,
			&customer.C_LAST,
			&customer.C_BALANCE,
		)
		customers = append(customers, customer)
	}

	if len(customers) < 1 {
		return nil, fmt.Errorf("no customers found with given name: %s", name)
	}

	return &customers[(len(customers)-1)/2],nil
}

func (db *SQLite) GetLastOrder(customerId int, warehouseId int, districtId int) (*models.Order, error) {
	query := "SELECT O_ID, O_CARRIER_ID, O_ENTRY_D FROM ORDERS WHERE O_W_ID = ? AND O_D_ID = ? AND O_C_ID = ?"

	row := db.queryRow(query, warehouseId, districtId, customerId)

	var m models.Order

	err := row.Scan(&m.O_ID, &m.O_CARRIER_ID, &m.O_ENTRY_D)
	if err != nil {
		return nil, err
	}

	return &m, nil
}

func (db *SQLite) GetOrderLines(orderId int, warehouseId int, districtId int) (*[]models.OrderLine, error) {
	query := "SELECT OL_O_ID, OL_D_ID, OL_W_ID, OL_NUMBER, OL_I_ID, OL_SUPPLY_W_ID, OL_DELIVERY_D, OL_QUANTITY, OL_AMOUNT, OL_DIST_INFO FROM ORDER_LINE " +
		"WHERE OL_O_ID = ? AND OL_W_ID = ? AND OL_D_ID = ?"

	rows, err := db.query(query, orderId, warehouseId, districtId)
	defer rows.Close()
	if err != nil {
		return nil, err
	}


	var ol []models.OrderLine

	for rows.Next() {
		var o models.OrderLine
		err = rows.Scan(
			&o.OL_O_ID,
			&o.OL_D_ID,
			&o.OL_W_ID,
			&o.OL_NUMBER,
			&o.OL_I_ID,
			&o.OL_SUPPLY_W_ID,
			&o.OL_DELIVERY_D,
			&o.OL_QUANTITY,
			&o.OL_AMOUNT,
			&o.OL_DIST_INFO,
		)
		if err != nil {
			return nil, err
		}

		ol = append(ol, o)
	}

	return &ol, nil
}

func (db *SQLite) GetWarehouse(warehouseId int) (*models.Warehouse, error) {
	query := "SELECT W_ID, W_NAME, W_STREET_1, W_STREET_2, W_CITY, W_STATE, W_ZIP, W_TAX, W_YTD FROM WAREHOUSE WHERE W_ID = ?"

	row := db.queryRow(query, warehouseId)

	var w models.Warehouse

	err := row.Scan(&w.W_ID, &w.W_NAME, &w.W_STREET_1, &w.W_STREET_2, &w.W_CITY, &w.W_STATE, &w.W_ZIP, &w.W_TAX, &w.W_YTD, )
	if err != nil {
		return nil, err
	}

	return &w, nil
}

func (db *SQLite) UpdateWarehouseBalance(warehouseId int, amount float64) error {
	query := "UPDATE WAREHOUSE SET W_YTD = W_YTD + ? WHERE W_ID = ?"

	r, err := db.exec(query, amount, warehouseId)
	if err != nil {
		return err
	}

	n, _ := r.RowsAffected()
	if n == 0 {
		return fmt.Errorf("unable to match warehouse")
	}


	return nil
}

func (db *SQLite) GetDistrict(warehouseId int, districtId int) (*models.District, error) {
	var query string
	if db.transactions {
		query = "SELECT D_ID, D_W_ID, D_NAME, D_STREET_1, D_STREET_2, D_CITY, D_STATE, D_ZIP, D_TAX, D_YTD, D_NEXT_O_ID FROM DISTRICT WHERE D_W_ID = ? and D_ID = ? FOR UPDATE"
	} else {
		query = "SELECT D_ID, D_W_ID, D_NAME, D_STREET_1, D_STREET_2, D_CITY, D_STATE, D_ZIP, D_TAX, D_YTD, D_NEXT_O_ID FROM DISTRICT WHERE D_W_ID = ? and D_ID = ?"
	}

	r := db.queryRow(query, warehouseId, districtId)
	var d models.District

	err := r.Scan(
		&d.D_ID,
		&d.D_W_ID,
		&d.D_NAME,
		&d.D_STREET_1,
		&d.D_STREET_2,
		&d.D_CITY,
		&d.D_STATE,
		&d.D_ZIP,
		&d.D_TAX,
		&d.D_YTD,
		&d.D_NEXT_O_ID,
	)

	if err != nil {
		return nil, err
	}

	return &d, nil
}

func (db *SQLite) UpdateDistrictBalance(warehouseId int, districtId int, amount float64) error {
	query := "UPDATE DISTRICT SET D_YTD = D_YTD + ? WHERE D_W_ID = ? AND D_ID = ?"

	r, err := db.exec(query, amount, warehouseId, districtId)
	if err != nil {
		return err
	}

	n, _ := r.RowsAffected()
	if n == 0 {
		return fmt.Errorf("Unable to match district")
	}

	return nil
}

func (db *SQLite) InsertHistory(warehouseId int, districtId int, date time.Time, amount float64, data string) error {
	query := "INSERT INTO HISTORY (H_C_ID, H_D_ID, H_W_ID, H_C_W_ID, H_C_D_ID, H_DATE, H_AMOUNT, H_DATA) VALUES (?,?,?,?,?,?,?,?)"

	_,err := db.exec(query, 1, districtId, warehouseId, warehouseId, districtId, date, amount, data)
	if err != nil {
		fmt.Println(db.transformQuery(query, 1, districtId, warehouseId, warehouseId, districtId, date, amount, data))
		panic("aa")
		return err
	}

	return nil
}

func (db *SQLite) UpdateCredit(customerId int, warehouseId int, districtId int, balance float64, data string) error {
	var err error
	var res sql.Result

	if len(data) > 0 {
		res, err = db.exec("UPDATE CUSTOMER SET " +
			"C_BALANCE = C_BALANCE + ?, C_YTD_PAYMENT = C_YTD_PAYMENT + ?, C_PAYMENT_CNT = C_PAYMENT_CNT + ?, C_DATA = ? " +
			"WHERE C_ID = ? AND C_W_ID = ? AND C_D_ID = ?",
			-1* balance,
			balance,
			1,
			data,
			customerId,
			warehouseId,
			districtId,
		)
	} else {
		res, err = db.exec("UPDATE CUSTOMER SET " +
			"C_BALANCE = C_BALANCE + ?, C_YTD_PAYMENT = C_YTD_PAYMENT + ?, C_PAYMENT_CNT = C_PAYMENT_CNT + ? " +
			"WHERE C_ID = ? AND C_W_ID = ? AND C_D_ID = ?",
			-1* balance,
			balance,
			1,
			customerId,
			warehouseId,
			districtId,
		)
	}

	if err != nil {
		return err
	}

	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("no customers matched")
	}

	return nil
}

func (db *SQLite) CreateOrder(
	orderId, customerId, warehouseId, districtId, oCarrierId, oOlCnt, allLocal int,
	orderEntryDate time.Time,
	orderLine []models.OrderLine,
) error {
	query := "INSERT INTO ORDERS (O_ID, O_C_ID, O_D_ID, O_W_ID, O_ENTRY_D, O_CARRIER_ID, O_OL_CNT, O_ALL_LOCAL) VALUES (?, ?, ?, ?, ?, ?, ?, ?)"

	_, err := db.exec(query, orderId, customerId, districtId, warehouseId, orderEntryDate, oCarrierId, oOlCnt, allLocal)

	if err != nil {
		fmt.Println(orderId, customerId, districtId, warehouseId, orderEntryDate, oCarrierId, oOlCnt, allLocal)
		return err
	}

	query = "INSERT INTO NEW_ORDERS (NO_O_ID, NO_D_ID, NO_W_ID) VALUES (?, ?, ?)"
	_,err = db.exec(query, orderId, districtId, warehouseId)
	if err != nil {
		return err
	}

	for _, o := range orderLine {
		query = "INSERT INTO ORDER_LINE (OL_O_ID, OL_D_ID, OL_W_ID, OL_NUMBER, OL_I_ID, OL_SUPPLY_W_ID, OL_QUANTITY, OL_AMOUNT, OL_DIST_INFO) " +
			"VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)"

		_, err = db.exec(query, o.OL_O_ID, districtId, warehouseId, o.OL_NUMBER, o.OL_I_ID, o.OL_SUPPLY_W_ID, o.OL_QUANTITY, o.OL_AMOUNT, o.OL_DIST_INFO)
		if err != nil {

			return err
		}
	}

	return nil
}

func (db *SQLite) GetItems(itemIds []int) (*[]models.Item, error) {
	var itemIds_ []string

	for _,item := range itemIds {
		itemIds_ = append(itemIds_, strconv.Itoa(item))
	}

	query := fmt.Sprintf("SELECT I_PRICE, I_NAME, I_DATA FROM ITEM WHERE I_ID IN (%s)", strings.Join(itemIds_, ","))

	rows, err := db.query(query)

	defer rows.Close()
	if err != nil {
		return nil, err
	}
	var items []models.Item

	for rows.Next() {
		var item models.Item

		err = rows.Scan(&item.I_PRICE, &item.I_NAME, &item.I_DATA)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return &items, nil
}

func (db *SQLite) UpdateStock(stockId int, warehouseId int, quantity int, ytd int, ordercnt int, remotecnt int) error {

	query := "UPDATE STOCK SET S_QUANTITY = ?, S_YTD = ?, S_ORDER_CNT = ?, S_REMOTE_CNT = ? WHERE S_I_ID = ? AND S_W_ID = ?"

	r, err := db.exec(query, quantity, ytd, ordercnt, remotecnt, stockId, warehouseId)
	if err != nil {
		return err
	}

	n, _ := r.RowsAffected()
	if n == 0 {
		return fmt.Errorf("unable to match stock")
	}

	return nil
}

func (db *SQLite) GetStockInfo(
	districtId int,
	iIds []int,
	iWids []int,
	allLocal int,
) (*[]models.Stock, error) {
	var buf string

	if allLocal == 1 {
		var iIds_ []string

		for _, item := range iIds {
			iIds_ = append(iIds_, strconv.Itoa(item))
		}

		buf = fmt.Sprintf(" S_W_ID = %d AND S_I_ID IN (%s)", iWids[0], strings.Join(iIds_, ","))

	} else {
		var p []string

		for i,item := range iIds {
			p = append(p, fmt.Sprintf("(S_W_ID = %d AND S_I_ID = %d)", iWids[i], item))
		}

		buf = strings.Join(p, " OR ")
	}

	query := fmt.Sprintf("SELECT S_I_ID, S_W_ID, S_QUANTITY, S_DATA, S_YTD, S_ORDER_CNT, S_REMOTE_CNT, S_DIST_%02d FROM STOCK " +
		"WHERE %s", districtId, buf)

	rows, err := db.query(query)
	defer rows.Close()
	if err != nil {
		return nil, err
	}

	var stocks []models.Stock
	for rows.Next() {
		var stock models.Stock

		var distcol *string

		switch districtId {
		case 1:
			distcol = &stock.S_DIST_01
		case 2:
			distcol = &stock.S_DIST_02
		case 3:
			distcol = &stock.S_DIST_03
		case 4:
			distcol = &stock.S_DIST_04
		case 5:
			distcol = &stock.S_DIST_05
		case 6:
			distcol = &stock.S_DIST_06
		case 7:
			distcol = &stock.S_DIST_07
		case 8:
			distcol = &stock.S_DIST_08
		case 9:
			distcol = &stock.S_DIST_09
		case 10:
			distcol = &stock.S_DIST_10
		default:
			panic("incorrect districtId")
		}

		err = rows.Scan(&stock.S_I_ID, &stock.S_W_ID, &stock.S_QUANTITY, &stock.S_DATA, &stock.S_YTD, &stock.S_ORDER_CNT, &stock.S_REMOTE_CNT, distcol)
		if err != nil {
			return nil, err
		}
		stocks = append(stocks, stock)

	}
	return &stocks, nil
}

func printCluster() {
	var leader_ni *protocol.NodeInfo
	var err error
	var servers []protocol.NodeInfo

	fmt.Println("Printing cluster..")

	if leader_cli != nil {
		fmt.Println("From Leader:")
		leader_ni, err = leader_cli.Leader(context.Background())
		if err == nil {
			fmt.Println(leader_ni.ID, " at ", leader_ni.Address)
		}

		servers, err = leader_cli.Cluster(context.Background())
		if err == nil {
			for _, ni := range servers {
				fmt.Printf("%s--%s,", ni.Address, ni.Role)
			}
		} else {
			fmt.Println("Error in leader node")
		}
		fmt.Println("\n-----------------")
	}

	for i, v := range voter_cli {
		fmt.Printf("(%d) From Node %s:", i, v)
		leader_ni, err = v.Leader(context.Background())
		if err == nil {
			fmt.Println("My leader is : ", leader_ni.ID, " at ", leader_ni.Address)
		}
	
		servers, err = v.Cluster(context.Background())
		if err == nil {
			for _, ni := range servers {
				fmt.Printf("%s--%s,", ni.Address, ni.Role)
			}
		} else {
			fmt.Println("Error in node ", i, " at ", v)
		}
		fmt.Println("\n-----------------")
	}
}