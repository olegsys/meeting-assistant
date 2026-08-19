// Интеграционные тесты репозиториев поверх реального PostgreSQL,
// поднимаемого через testcontainers (см. internal/testdb).
//
//go:build integration

package repository_test

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/olegsys/meeting-assistant/internal/models"
	"github.com/olegsys/meeting-assistant/internal/repository"
	"github.com/olegsys/meeting-assistant/internal/testdb"
)

type repos struct {
	pool        *pgxpool.Pool
	userRepo    repository.UserRepo
	meetingRepo repository.MeetingRepo
	fileRepo    repository.FileRepo
	taskRepo    repository.TaskRepo
	contentRepo repository.ContentRepo
	chatRepo    repository.ChatRepo
}

func newRepos(t *testing.T) *repos {
	t.Helper()

	pool := testdb.Pool(t)
	testdb.Truncate(t, pool)

	return &repos{
		pool:        pool,
		userRepo:    repository.NewUserRepo(pool),
		meetingRepo: repository.NewMeetingRepo(pool),
		fileRepo:    repository.NewFileRepo(pool),
		taskRepo:    repository.NewTaskRepo(pool),
		contentRepo: repository.NewContentRepo(pool),
		chatRepo:    repository.NewChatRepo(pool),
	}
}

// UserRepo — создание, поиск, идемпотентность Create через ON CONFLICT.

func TestUserRepo_CreateAndGetByExternalID(t *testing.T) {
	r := newRepos(t)
	ctx := context.Background()

	_, err := r.userRepo.GetByExternalID(ctx, "missing")
	if !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("ожидалось ErrNotFound, получено %v", err)
	}

	created, err := r.userRepo.Create(ctx, "alice")
	if err != nil {
		t.Fatalf("Create alice: %v", err)
	}

	if created.ID == 0 || created.ExternalID != "alice" {
		t.Fatalf("неожиданный пользователь: %+v", created)
	}

	found, err := r.userRepo.GetByExternalID(ctx, "alice")
	if err != nil {
		t.Fatalf("GetByExternalID alice: %v", err)
	}

	if found.ID != created.ID {
		t.Fatalf("id не совпадает: %d != %d", found.ID, created.ID)
	}

	again, err := r.userRepo.Create(ctx, "alice")
	if err != nil {
		t.Fatalf("повторный Create alice: %v", err)
	}

	if again.ID != created.ID {
		t.Fatalf("повторный Create вернул другой id: %d != %d", again.ID, created.ID)
	}
}

// MeetingRepo — создание встреч, фильтрация списка по user_id.

func TestMeetingRepo_CreateAndListByUser(t *testing.T) {
	r := newRepos(t)
	ctx := context.Background()

	alice := testdb.MustInsertUser(t, r.pool, "alice")
	bob := testdb.MustInsertUser(t, r.pool, "bob")

	m1, err := r.meetingRepo.Create(ctx, alice, "Встреча 1")
	if err != nil {
		t.Fatalf("Create m1: %v", err)
	}

	if _, err := r.taskRepo.Create(ctx, m1.ID); err != nil {
		t.Fatalf("Create task m1: %v", err)
	}

	m2, err := r.meetingRepo.Create(ctx, alice, "Встреча 2")
	if err != nil {
		t.Fatalf("Create m2: %v", err)
	}

	if _, err := r.taskRepo.Create(ctx, m2.ID); err != nil {
		t.Fatalf("Create task m2: %v", err)
	}

	m3, err := r.meetingRepo.Create(ctx, bob, "Встреча Bob")
	if err != nil {
		t.Fatalf("Create m3: %v", err)
	}

	if _, err := r.taskRepo.Create(ctx, m3.ID); err != nil {
		t.Fatalf("Create task m3: %v", err)
	}

	aliceItems, err := r.meetingRepo.List(ctx, alice)
	if err != nil {
		t.Fatalf("List alice: %v", err)
	}

	if len(aliceItems) != 2 {
		t.Fatalf("alice: ожидалось 2 встречи, получено %d", len(aliceItems))
	}

	for _, item := range aliceItems {
		if item.Title == "Встреча Bob" {
			t.Fatalf("alice увидел встречу Bob: %+v", item)
		}
	}

	bobItems, err := r.meetingRepo.List(ctx, bob)
	if err != nil {
		t.Fatalf("List bob: %v", err)
	}

	if len(bobItems) != 1 || bobItems[0].MeetingID != m3.ID {
		t.Fatalf("bob: ожидалась 1 встреча m3=%d, получено %+v", m3.ID, bobItems)
	}
}

// FileRepo — сохранение и чтение BYTEA.

func TestFileRepo_SaveAndGet(t *testing.T) {
	r := newRepos(t)
	ctx := context.Background()

	uid := testdb.MustInsertUser(t, r.pool, "alice")
	mid := testdb.MustInsertMeeting(t, r.pool, uid, "audio.wav")

	payload := []byte{0x52, 0x49, 0x46, 0x46, 0xDE, 0xAD, 0xBE, 0xEF}

	if err := r.fileRepo.Save(ctx, mid, "audio.wav", payload); err != nil {
		t.Fatalf("Save: %v", err)
	}

	name, content, err := r.fileRepo.Get(ctx, mid)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if name != "audio.wav" {
		t.Fatalf("file_name = %q, ожидалось audio.wav", name)
	}

	if len(content) != len(payload) {
		t.Fatalf("длина: %d != %d", len(content), len(payload))
	}

	for i := range payload {
		if content[i] != payload[i] {
			t.Fatalf("байт %d: %x != %x", i, content[i], payload[i])
		}
	}

	_, _, err = r.fileRepo.Get(ctx, 9999)
	if !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("ожидалось ErrNotFound, получено %v", err)
	}
}

// TaskRepo — ClaimCreated с SKIP LOCKED.

func TestTaskRepo_ClaimCreated_SkipLocked(t *testing.T) {
	r := newRepos(t)
	ctx := context.Background()

	uid := testdb.MustInsertUser(t, r.pool, "alice")

	mid1 := testdb.MustInsertMeeting(t, r.pool, uid, "m1")
	mid2 := testdb.MustInsertMeeting(t, r.pool, uid, "m2")
	mid3 := testdb.MustInsertMeeting(t, r.pool, uid, "m3")

	if _, err := r.taskRepo.Create(ctx, mid1); err != nil {
		t.Fatalf("create task1: %v", err)
	}

	if _, err := r.taskRepo.Create(ctx, mid2); err != nil {
		t.Fatalf("create task2: %v", err)
	}

	if _, err := r.taskRepo.Create(ctx, mid3); err != nil {
		t.Fatalf("create task3: %v", err)
	}

	var (
		mu      sync.Mutex
		allJobs [][]models.ProcessingJob
		wg      sync.WaitGroup
	)

	for i := 0; i < 2; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			jobs, err := r.taskRepo.ClaimCreated(ctx, 1)
			if err != nil {
				t.Errorf("ClaimCreated: %v", err)
				return
			}

			mu.Lock()
			allJobs = append(allJobs, jobs)
			mu.Unlock()

			time.Sleep(200 * time.Millisecond)
		}()
	}

	wg.Wait()

	var claimed []int64
	for _, jobs := range allJobs {
		for _, j := range jobs {
			claimed = append(claimed, j.TaskID)
		}
	}

	if len(claimed) != 2 {
		t.Fatalf("ожидалось 2 разных задачи, получено %d (claimed=%v)", len(claimed), claimed)
	}

	seen := make(map[int64]bool)
	for _, id := range claimed {
		if seen[id] {
			t.Fatalf("дубликат taskID %d — SKIP LOCKED не сработал", id)
		}

		seen[id] = true
	}

	jobs, err := r.taskRepo.ClaimCreated(ctx, 1)
	if err != nil {
		t.Fatalf("ClaimCreated повторно: %v", err)
	}

	if len(jobs) != 1 {
		t.Fatalf("третья задача не вернулась: jobs=%v", jobs)
	}
}

// TaskRepo — RequeueStale: requeue vs failed.

func TestTaskRepo_RequeueStale_RequeueAndFail(t *testing.T) {
	r := newRepos(t)
	ctx := context.Background()

	uid := testdb.MustInsertUser(t, r.pool, "alice")

	mid1 := testdb.MustInsertMeeting(t, r.pool, uid, "m1")
	mid2 := testdb.MustInsertMeeting(t, r.pool, uid, "m2")

	task1, err := r.taskRepo.Create(ctx, mid1)
	if err != nil {
		t.Fatalf("create task1: %v", err)
	}

	task2, err := r.taskRepo.Create(ctx, mid2)
	if err != nil {
		t.Fatalf("create task2: %v", err)
	}

	if _, err := r.taskRepo.ClaimCreated(ctx, 10); err != nil {
		t.Fatalf("ClaimCreated: %v", err)
	}

	testdb.MustExec(t, r.pool,
		`UPDATE processing_tasks SET attempts = $2, updated_at = now() - interval '1 hour' WHERE id = $1`,
		task1, 1,
	)

	testdb.MustExec(t, r.pool,
		`UPDATE processing_tasks SET attempts = $2, updated_at = now() - interval '1 hour' WHERE id = $1`,
		task2, 5,
	)

	requeued, err := r.taskRepo.RequeueStale(ctx, time.Minute, 3)
	if err != nil {
		t.Fatalf("RequeueStale: %v", err)
	}

	if requeued != 2 {
		t.Fatalf("requeued=%d, ожидалось 2", requeued)
	}

	var (
		st1, st2, msg2 string
	)

	if err := r.pool.QueryRow(ctx,
		`SELECT status, error_message FROM processing_tasks WHERE id = $1`, task1,
	).Scan(&st1, &msg2); err != nil {
		t.Fatalf("read task1: %v", err)
	}

	if st1 != string(models.StatusCreated) {
		t.Fatalf("task1.status = %q, ожидалось created", st1)
	}

	if err := r.pool.QueryRow(ctx,
		`SELECT status FROM processing_tasks WHERE id = $1`, task2,
	).Scan(&st2); err != nil {
		t.Fatalf("read task2: %v", err)
	}

	if st2 != string(models.StatusFailed) {
		t.Fatalf("task2.status = %q, ожидалось failed", st2)
	}

	var errMsg string
	if err := r.pool.QueryRow(ctx,
		`SELECT error_message FROM processing_tasks WHERE id = $1`, task2,
	).Scan(&errMsg); err != nil {
		t.Fatalf("read task2 msg: %v", err)
	}

	if errMsg == "" {
		t.Fatalf("task2: error_message пуст")
	}
}

// TaskRepo — SetFailed сохраняет сообщение.

func TestTaskRepo_SetFailed_StoresMessage(t *testing.T) {
	r := newRepos(t)
	ctx := context.Background()

	uid := testdb.MustInsertUser(t, r.pool, "alice")
	mid := testdb.MustInsertMeeting(t, r.pool, uid, "m")
	taskID, err := r.taskRepo.Create(ctx, mid)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	var beforeUpdate time.Time
	if err := r.pool.QueryRow(ctx,
		`SELECT updated_at FROM processing_tasks WHERE id = $1`, taskID,
	).Scan(&beforeUpdate); err != nil {
		t.Fatalf("read updated_at: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	if err := r.taskRepo.SetFailed(ctx, taskID, "boom"); err != nil {
		t.Fatalf("SetFailed: %v", err)
	}

	var (
		status string
		msg    string
		upd    time.Time
	)

	if err := r.pool.QueryRow(ctx,
		`SELECT status, error_message, updated_at FROM processing_tasks WHERE id = $1`, taskID,
	).Scan(&status, &msg, &upd); err != nil {
		t.Fatalf("read task: %v", err)
	}

	if status != string(models.StatusFailed) {
		t.Fatalf("status = %q, ожидалось failed", status)
	}

	if msg != "boom" {
		t.Fatalf("error_message = %q, ожидалось boom", msg)
	}

	if !upd.After(beforeUpdate) {
		t.Fatalf("updated_at не сдвинулся вперёд: before=%v after=%v", beforeUpdate, upd)
	}

	err = r.taskRepo.SetFailed(ctx, 9999, "x")
	if !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("SetFailed на несуществующий id: ожидалось ErrNotFound, получено %v", err)
	}
}

// ContentRepo — UpsertTranscription идемпотентность.

func TestContentRepo_UpsertTranscription_Idempotent(t *testing.T) {
	r := newRepos(t)
	ctx := context.Background()

	uid := testdb.MustInsertUser(t, r.pool, "alice")
	mid := testdb.MustInsertMeeting(t, r.pool, uid, "m")

	if err := r.contentRepo.UpsertTranscription(ctx, mid, "первая"); err != nil {
		t.Fatalf("первая вставка: %v", err)
	}

	err := r.contentRepo.UpsertTranscription(ctx, mid, "вторая")
	if !errors.Is(err, models.ErrTranscriptionAlreadyExists) {
		t.Fatalf("повторная вставка: ожидалось ErrTranscriptionAlreadyExists, получено %v", err)
	}

	got, err := r.contentRepo.GetTranscriptionOwned(ctx, uid, mid)
	if err != nil {
		t.Fatalf("GetTranscriptionOwned: %v", err)
	}

	if got != "первая" {
		t.Fatalf("содержимое = %q, ожидалось 'первая'", got)
	}
}

// ContentRepo — UpsertSummary идемпотентность.

func TestContentRepo_UpsertSummary_Idempotent(t *testing.T) {
	r := newRepos(t)
	ctx := context.Background()

	uid := testdb.MustInsertUser(t, r.pool, "alice")
	mid := testdb.MustInsertMeeting(t, r.pool, uid, "m")

	if err := r.contentRepo.UpsertSummary(ctx, mid, "выжимка 1"); err != nil {
		t.Fatalf("первая вставка: %v", err)
	}

	err := r.contentRepo.UpsertSummary(ctx, mid, "выжимка 2")
	if !errors.Is(err, models.ErrSummaryAlreadyExists) {
		t.Fatalf("повторная вставка: ожидалось ErrSummaryAlreadyExists, получено %v", err)
	}

	_, summary, err := r.contentRepo.GetContextOwned(ctx, uid, mid)
	if err != nil {
		t.Fatalf("GetContextOwned: %v", err)
	}

	if summary != "выжимка 1" {
		t.Fatalf("summary = %q, ожидалось 'выжимка 1'", summary)
	}
}

// MeetingRepo.Find — FTS и изоляция по пользователю.

func TestMeetingRepo_Find_FTS_AndIsolation(t *testing.T) {
	r := newRepos(t)
	ctx := context.Background()

	alice := testdb.MustInsertUser(t, r.pool, "alice")
	bob := testdb.MustInsertUser(t, r.pool, "bob")

	midA := testdb.MustInsertMeeting(t, r.pool, alice, "Бюджет Q3")
	taskA, err := r.taskRepo.Create(ctx, midA)
	if err != nil {
		t.Fatalf("taskA: %v", err)
	}

	if err := r.contentRepo.UpsertTranscription(ctx, midA,
		"Обсудили бюджет на третий квартал и приняли решение увеличить маркетинг.",
	); err != nil {
		t.Fatalf("upsert trans A: %v", err)
	}

	_ = r.taskRepo.UpdateStatus(ctx, taskA, models.StatusTranscribed)

	midB := testdb.MustInsertMeeting(t, r.pool, bob, "Бюджет Bob")
	taskB, err := r.taskRepo.Create(ctx, midB)
	if err != nil {
		t.Fatalf("taskB: %v", err)
	}

	if err := r.contentRepo.UpsertTranscription(ctx, midB,
		"Обсудили бюджет отдела разработки и найм новых инженеров.",
	); err != nil {
		t.Fatalf("upsert trans B: %v", err)
	}

	_ = r.taskRepo.UpdateStatus(ctx, taskB, models.StatusTranscribed)

	resultsAlice, err := r.meetingRepo.Find(ctx, alice, "бюджет")
	if err != nil {
		t.Fatalf("Find alice: %v", err)
	}

	if len(resultsAlice) != 1 {
		t.Fatalf("alice: ожидалось 1, получено %d (%+v)", len(resultsAlice), resultsAlice)
	}

	if resultsAlice[0].MeetingID != midA {
		t.Fatalf("alice нашёл чужую встречу: %+v", resultsAlice[0])
	}

	resultsBob, err := r.meetingRepo.Find(ctx, bob, "бюджет")
	if err != nil {
		t.Fatalf("Find bob: %v", err)
	}

	if len(resultsBob) != 1 || resultsBob[0].MeetingID != midB {
		t.Fatalf("bob: ожидалась встреча %d, получено %+v", midB, resultsBob)
	}
}

// MeetingRepo.Find — нет совпадений.

func TestMeetingRepo_Find_NoMatch(t *testing.T) {
	r := newRepos(t)
	ctx := context.Background()

	uid := testdb.MustInsertUser(t, r.pool, "alice")
	mid := testdb.MustInsertMeeting(t, r.pool, uid, "Какая-то встреча")
	if _, err := r.taskRepo.Create(ctx, mid); err != nil {
		t.Fatalf("create task: %v", err)
	}

	results, err := r.meetingRepo.Find(ctx, uid, "абсолютно_чужое_слово")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	if len(results) != 0 {
		t.Fatalf("ожидалось 0 результатов, получено %d", len(results))
	}
}

// ContentRepo.GetTranscriptionOwned — изоляция по владельцу.

func TestContentRepo_GetTranscriptionOwned(t *testing.T) {
	r := newRepos(t)
	ctx := context.Background()

	alice := testdb.MustInsertUser(t, r.pool, "alice")
	bob := testdb.MustInsertUser(t, r.pool, "bob")

	midAlice := testdb.MustInsertMeeting(t, r.pool, alice, "alice meeting")
	if err := r.contentRepo.UpsertTranscription(ctx, midAlice, "секретные данные alice"); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	transcript, err := r.contentRepo.GetTranscriptionOwned(ctx, alice, midAlice)
	if err != nil {
		t.Fatalf("alice get own: %v", err)
	}

	if transcript != "секретные данные alice" {
		t.Fatalf("alice транскрипция = %q", transcript)
	}

	_, err = r.contentRepo.GetTranscriptionOwned(ctx, bob, midAlice)
	if !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("bob чужая встреча: ожидалось ErrNotFound, получено %v", err)
	}
}

// Сортируем список по id, чтобы избежать ложных падений при изменении порядка.
func sortedIDs(jobs []models.ProcessingJob) []int64 {
	ids := make([]int64, 0, len(jobs))
	for _, j := range jobs {
		ids = append(ids, j.TaskID)
	}

	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	return ids
}

var _ = sortedIDs
