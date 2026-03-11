package main

import (
	"context"
	"fmt"
	"reflect"
	"time"
)

type User struct {
	Name string
}

func main() {
	ctx := context.Background()
	user := User{Name: "heijior"}
	res, cancel := func(ctx context.Context, val any) (context.Context, context.CancelFunc) {
		ctx1, cancel := context.WithDeadline(ctx, time.Now().Add(time.Second*3))
		if v, ok := val.(*User); ok {
			s := reflect.TypeOf(v).Elem()
			str := make([]string, 0, s.NumField())
			for i := 0; i < s.NumField(); i++ {
				str = append(str, s.Field(i).Name)
			}
			ctx2 := context.WithValue(ctx1, "name", str)
			return ctx2, cancel
		}
		return ctx1, cancel

	}(ctx, &user)
	defer cancel()
	for {
		select {
		case <-res.Done():
			fmt.Printf("value is %s", res.Value("name"))
			fmt.Printf("Context 结束，原因：%v\n", res.Err())
			return
		default:
			fmt.Println("程序运行中...")
			time.Sleep(1 * time.Second)
		}
	}

}
