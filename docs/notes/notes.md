## SQLITE research notes


-unlike the traditional databases (postgress or mysql) which run as seprate process and communicate via a shared memmory or network protocol. The SQLiteDB is a in-process database which runs along the main process

-SQLite uses a row-oriented storage format,
where all the columns of a given record are stored in a contiguous
memory region

-An entire  SQLite database is stored in a single file. which support cros platform 

- SQLite is Compact & self contained which means it does not require external deps and no configs

-A SQLite database is highly reliable cause it uses 600 lines of test code for 1 line of actual db code 

-SQLite has a modular acchiteture means its architeture is divided into highly decoupled modules 