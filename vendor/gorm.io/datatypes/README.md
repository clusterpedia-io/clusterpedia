# GORM Data Types

## JSON

sqlite, mysql, postgres supported

```go
import "gorm.io/datatypes"

type UserWithJSON struct {
	gorm.Model
	Name       string
	Attributes datatypes.JSON
}

DB.Create(&User{
	Name:       "json-1",
	Attributes: datatypes.JSON([]byte(`{"name": "jinzhu", "age": 18, "tags": ["tag1", "tag2"], "orgs": {"orga": "orga"}}`)),
}

// Check JSON has keys
datatypes.JSONQuery("attributes").HasKey(value, keys...)

db.Find(&user, datatypes.JSONQuery("attributes").HasKey("role"))
db.Find(&user, datatypes.JSONQuery("attributes").HasKey("orgs", "orga"))
// MySQL
// SELECT * FROM `users` WHERE JSON_EXTRACT(`attributes`, '$.role') IS NOT NULL
// SELECT * FROM `users` WHERE JSON_EXTRACT(`attributes`, '$.orgs.orga') IS NOT NULL

// PostgreSQL
// SELECT * FROM "user" WHERE "attributes"::jsonb ? 'role'
// SELECT * FROM "user" WHERE "attributes"::jsonb -> 'orgs' ? 'orga'


// Check JSON extract value from keys equal to value
datatypes.JSONQuery("attributes").Equals(value, keys...)

DB.First(&user, datatypes.JSONQuery("attributes").Equals("jinzhu", "name"))
DB.First(&user, datatypes.JSONQuery("attributes").Equals("orgb", "orgs", "orgb"))
// MySQL
// SELECT * FROM `user` WHERE JSON_EXTRACT(`attributes`, '$.name') = "jinzhu"
// SELECT * FROM `user` WHERE JSON_EXTRACT(`attributes`, '$.orgs.orgb') = "orgb"

// PostgreSQL
// SELECT * FROM "user" WHERE json_extract_path_text("attributes"::json,'name') = 'jinzhu'
// SELECT * FROM "user" WHERE json_extract_path_text("attributes"::json,'orgs','orgb') = 'orgb'
```

NOTE: SQlite need to build with `json1` tag, e.g: `go build --tags json1`, refer https://github.com/mattn/go-sqlite3#usage

## Date

```go
import "gorm.io/datatypes"

type UserWithDate struct {
	gorm.Model
	Name string
	Date datatypes.Date
}

user := UserWithDate{Name: "jinzhu", Date: datatypes.Date(time.Now())}
DB.Create(&user)
// INSERT INTO `user_with_dates` (`name`,`date`) VALUES ("jinzhu","2020-07-17 00:00:00")

DB.First(&result, "name = ? AND date = ?", "jinzhu", datatypes.Date(curTime))
// SELECT * FROM user_with_dates WHERE name = "jinzhu" AND date = "2020-07-17 00:00:00" ORDER BY `user_with_dates`.`id` LIMIT 1
```

## Time

MySQL, PostgreSQL, SQLite, SQLServer are supported.

Time with nanoseconds is supported for some databases which support for time with fractional second scale.

```go
import "gorm.io/datatypes"

type UserWithTime struct {
    gorm.Model
    Name string
    Time datatypes.Time
}

user := UserWithTime{Name: "jinzhu", Time: datatypes.NewTime(1, 2, 3, 0)}
DB.Create(&user)
// INSERT INTO `user_with_times` (`name`,`time`) VALUES ("jinzhu","01:02:03")

DB.First(&result, "name = ? AND time = ?", "jinzhu", datatypes.NewTime(1, 2, 3, 0))
// SELECT * FROM user_with_times WHERE name = "jinzhu" AND time = "01:02:03" ORDER BY `user_with_times`.`id` LIMIT 1
```

NOTE: If the current using database is SQLite, the field column type is defined as `TEXT` type
when GORM AutoMigrate because SQLite doesn't have time type.
