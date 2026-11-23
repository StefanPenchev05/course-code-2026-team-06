package application

import (
	"context"
	"errors"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	scheduling "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/application"
)

var ErrWatch=errors.New("availability watch not permitted or watch limit reached")
type Visibility interface { CanViewAvailability(context.Context,domain.Principal,string)(bool,error) }
type Watches interface {
	Register(user,tutor string)(time.Time,error)
	Users(tutor string)[]string
	Contains(user,tutor string)bool
}
type Routing struct { bookings scheduling.RealtimeQueries; visibility Visibility; watches Watches }
func NewRouting(bookings scheduling.RealtimeQueries,visibility Visibility,watches Watches)*Routing{return &Routing{bookings,visibility,watches}}

func(r *Routing)Watch(ctx context.Context,p domain.Principal,tutor string)(time.Time,error){
	if !p.VerifiedStudent()||!ValidID(tutor){return time.Time{},ErrWatch}
	ok,err:=r.visibility.CanViewAvailability(ctx,p,tutor);if err!=nil{return time.Time{},err};if !ok{return time.Time{},ErrWatch}
	return r.watches.Register(p.ID,tutor)
}
func(r *Routing)Recipients(ctx context.Context,e Event)([]string,error){
	switch e.Type{
	case BookingChanged:
		a,err:=r.bookings.BookingAudience(ctx,e.EntityID);if err!=nil{return nil,err};if a.StudentID==""{return nil,nil}
		ids:=[]string{a.StudentID,a.TutorID};if a.CoordinatorID!=""{ids=append(ids,a.CoordinatorID)};return ids,nil
	case AvailabilityChanged:return append(r.watches.Users(e.EntityID),e.EntityID),nil
	default:return nil,ErrUnknownType
	}
}
func(r *Routing)Authorize(ctx context.Context,p domain.Principal,e Event)(bool,error){
	if !p.Active(){return false,nil}
	switch e.Type{
	case BookingChanged:
		a,err:=r.bookings.BookingAudience(ctx,e.EntityID);if err!=nil{return false,err};if a.StudentID==""{return false,nil}
		return (p.VerifiedStudent()&&(p.ID==a.StudentID||p.ID==a.TutorID))||
			(p.Coordinator()&&p.ID==a.CoordinatorID&&p.CanAccessFaculty(a.FacultyID)),nil
	case AvailabilityChanged:
		if p.ID==e.EntityID{return p.CanTutor(),nil}
		if !p.VerifiedStudent()||!r.watches.Contains(p.ID,e.EntityID){return false,nil}
		return r.visibility.CanViewAvailability(ctx,p,e.EntityID)
	default:return false,ErrUnknownType
	}
}

// Receiver is owned by one subscriber loop. Its bounded cache suppresses local
// duplicate queues, but event IDs and mandatory refetch remain the end-to-end rule.
type Receiver struct {
	store Outbox; routing *Routing; publisher Publisher
	seen map[string]bool; ring []string; next int
}
func NewReceiver(store Outbox,routing *Routing,publisher Publisher)*Receiver{
	return &Receiver{store:store,routing:routing,publisher:publisher,seen:map[string]bool{},ring:make([]string,4096)}
}
func(r *Receiver)Receive(ctx context.Context,id string)error{
	if !ValidID(id){return ErrInvalidEvent};if r.seen[id]{return nil}
	e,err:=r.store.Get(ctx,id);if err!=nil{return err};if err:=e.Validate();err!=nil{return err}
	users,err:=r.routing.Recipients(ctx,e);if err!=nil{return err}
	// The hub accepts at most 1024 candidates per call. Batches never broaden
	// scope: every actual delivery still applies the resource policy.
	for len(users)>0{
		n:=len(users);if n>1024{n=1024}
		if err:=r.publisher.Publish(ctx,e,users[:n],r.routing.Authorize);err!=nil{return err};users=users[n:]
	}
	delete(r.seen,r.ring[r.next]);r.ring[r.next]=id;r.seen[id]=true;r.next=(r.next+1)%len(r.ring)
	return nil
}
