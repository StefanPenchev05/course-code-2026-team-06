package memory

import (
	"sync"
	"time"

	app "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/realtime/application"
)

const WatchTTL=10*time.Minute
type Watches struct{mu sync.Mutex; now func()time.Time; entries map[string]map[string]time.Time; total int}
func NewWatches(now func()time.Time)*Watches{return &Watches{now:now,entries:map[string]map[string]time.Time{}}}
func(w *Watches)prune(){
	now:=w.now();for user,tutors:=range w.entries{for tutor,end:=range tutors{if !end.After(now){delete(tutors,tutor);w.total--}};if len(tutors)==0{delete(w.entries,user)}}
}
func(w *Watches)Register(user,tutor string)(time.Time,error){
	w.mu.Lock();defer w.mu.Unlock();w.prune()
	if !app.ValidID(user)||!app.ValidID(tutor){return time.Time{},app.ErrWatch}
	entries:=w.entries[user];_,exists:=entries[tutor]
	if !exists&&(len(entries)>=32||w.total>=16384){return time.Time{},app.ErrWatch}
	if entries==nil{entries=map[string]time.Time{};w.entries[user]=entries}
	end:=w.now().Add(WatchTTL);entries[tutor]=end;if !exists{w.total++};return end,nil
}
func(w *Watches)Users(tutor string)[]string{
	w.mu.Lock();defer w.mu.Unlock();w.prune();out:=[]string{}
	for user,entries:=range w.entries{if _,ok:=entries[tutor];ok{out=append(out,user)}};return out
}
func(w *Watches)Contains(user,tutor string)bool{
	w.mu.Lock();defer w.mu.Unlock();return w.entries[user][tutor].After(w.now())
}
