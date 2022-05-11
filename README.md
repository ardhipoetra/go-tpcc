# go-tpcc

## Changes from main repo
This repository changes postgreSQL implementation to go-sqlite implementation by [mattn](github.com/mattn/go-sqlite3).

## Preparing dataset
This example will create `a.db` with enabled shared-cache and WAL (so that no `database is locked` problem occurs). 8 threads with 2 warehouses give us in total 204 mb (.db, -wal, -shm) in one of my machine.
```
./go-tpcc prepare --db a --dbdriver sqlite --uri "cache=shared&_journal=WAL" --threads 8 --warehouses 2
```

## Running test
Assuming you already had `a.db` from before, this will run the test.
```
./go-tpcc run --db a --dbdriver sqlite --help --uri "cache=shared&_journal=WAL" --threads 8 --warehouses 2
```

The output : 
```
[ 1s ] TPS: 4149.00 StockLevel: 169 (4.32 ms) Delivery: 165 (3.98 ms) OrderStatus: 143 (4.44 ms) Payment: 1821 (4.75 ms) NewOrder: 1851 (9.65 ms) Failed: 2009
[ 2s ] TPS: 4346.00 StockLevel: 195 (3.27 ms) Delivery: 185 (5.37 ms) OrderStatus: 168 (3.84 ms) Payment: 1915 (5.20 ms) NewOrder: 1883 (9.04 ms) Failed: 2077
[ 3s ] TPS: 4197.00 StockLevel: 185 (3.50 ms) Delivery: 168 (4.55 ms) OrderStatus: 168 (4.78 ms) Payment: 1729 (5.32 ms) NewOrder: 1947 (9.38 ms) Failed: 2075
[ 4s ] TPS: 4131.00 StockLevel: 164 (5.52 ms) Delivery: 159 (5.92 ms) OrderStatus: 161 (3.61 ms) Payment: 1669 (5.18 ms) NewOrder: 1978 (9.92 ms) Failed: 2116
[ 5s ] TPS: 4274.00 StockLevel: 176 (6.75 ms) Delivery: 162 (5.10 ms) OrderStatus: 169 (5.25 ms) Payment: 1860 (4.90 ms) NewOrder: 1907 (9.53 ms) Failed: 2061
[ 6s ] TPS: 4245.00 StockLevel: 163 (4.82 ms) Delivery: 161 (4.21 ms) OrderStatus: 186 (4.77 ms) Payment: 1836 (4.52 ms) NewOrder: 1899 (9.53 ms) Failed: 2067
[ 7s ] TPS: 4269.00 StockLevel: 173 (2.12 ms) Delivery: 164 (4.05 ms) OrderStatus: 168 (4.78 ms) Payment: 1844 (4.84 ms) NewOrder: 1920 (9.78 ms) Failed: 2071
[ 8s ] TPS: 4292.00 StockLevel: 177 (3.11 ms) Delivery: 166 (5.37 ms) OrderStatus: 170 (4.06 ms) Payment: 1846 (5.20 ms) NewOrder: 1933 (9.76 ms) Failed: 2101
[ 9s ] TPS: 4331.00 StockLevel: 161 (3.56 ms) Delivery: 180 (4.28 ms) OrderStatus: 176 (4.17 ms) Payment: 1914 (5.15 ms) NewOrder: 1900 (9.31 ms) Failed: 2087
[ 10s ] TPS: 4288.00 StockLevel: 184 (3.32 ms) Delivery: 159 (3.58 ms) OrderStatus: 180 (4.54 ms) Payment: 1844 (4.99 ms) NewOrder: 1921 (9.37 ms) Failed: 2105

```

More complete options :

```
 ./go-tpcc help run
Run

Usage:
  go-tpcc run [flags]

Flags:
  -h, --help                   help for run
      --percent-fail int       How much % of New Order trxs should fail [0-100]
      --percentile int         Percentile for latency reporting (default 95)
      --report-format string   default|json|csv (default "default")
      --report-interval int    Report interval (default 1)
      --scalefactor float      Scale-factor (default 1)
      --threads int            Amount of threads that will be used when preparing. min(threads, warehouses) will be used at most (default 8)
      --time int               How long to run the test (default 10)
      --warehouses int         Number of warehouses to generate the data (default 10)

Global Flags:
      --db string    database name to use
      --trx          use trx?. false by default
      --uri string   DSN

```
