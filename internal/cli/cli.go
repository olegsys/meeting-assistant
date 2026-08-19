package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/olegsys/meeting-assistant/internal/httpclient"
)

// Runner исполняет CLI команды, делегируя их HTTP API сервиса.
type Runner struct {
	client      *httpclient.Client
	defaultUser string
}

// NewRunner создаёт Runner с адресом сервиса и пользователем по умолчанию.
func NewRunner(serviceAddress, defaultUser string) *Runner {
	return &Runner{
		client:      httpclient.New(serviceAddress),
		defaultUser: defaultUser,
	}
}

func (r *Runner) Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("команда не указана")
	}

	cmd := args[0]

	switch cmd {
	case "start":
		return r.start(ctx, args[1:])
	case "load":
		return r.load(ctx, args[1:])
	case "list":
		return r.list(ctx, args[1:])
	case "status":
		return r.status(ctx, args[1:])
	case "get":
		return r.get(ctx, args[1:])
	case "find":
		return r.find(ctx, args[1:])
	case "chat":
		return r.chat(ctx, args[1:])
	case "retry":
		return r.retry(ctx, args[1:])
	default:
		return fmt.Errorf("неизвестная команда: %s", cmd)
	}
}

func (r *Runner) start(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	user := fs.String("user", r.defaultUser, "external user id")
	_ = fs.Parse(args)

	if *user == "" {
		return errors.New("не указан user")
	}

	created, err := r.client.Start(ctx, *user)
	if err != nil {
		return err
	}

	fmt.Printf("user registered: id=%d external_id=%s\n", created.ID, created.ExternalID)

	return nil
}

func (r *Runner) load(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("load", flag.ExitOnError)
	user := fs.String("user", r.defaultUser, "external user id")
	path := fs.String("path", "", "path to file")
	_ = fs.Parse(args)

	if *user == "" {
		return errors.New("не указан user")
	}

	if *path == "" {
		return errors.New("не указан path")
	}

	result, err := r.client.Load(ctx, *user, *path)
	if err != nil {
		return err
	}

	fmt.Printf("meeting created: id=%d\n", result.MeetingID)
	fmt.Printf("status: %s\n", string(result.Status))

	return nil
}

func (r *Runner) list(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	user := fs.String("user", r.defaultUser, "external user id")
	_ = fs.Parse(args)

	if *user == "" {
		return errors.New("не указан user")
	}

	items, err := r.client.List(ctx, *user)
	if err != nil {
		return err
	}

	if len(items) == 0 {
		fmt.Println("встреч нет")
		return nil
	}

	for _, item := range items {
		fmt.Printf(
			"%d\t%s\t%s\t%s\t%s\n",
			item.MeetingID,
			item.CreatedAt.Format(time.RFC3339),
			string(item.Status),
			item.Title,
			firstLine(item.Summary, 80),
		)
	}

	return nil
}

func (r *Runner) status(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	user := fs.String("user", r.defaultUser, "external user id")
	id := fs.Int64("id", 0, "meeting id")
	_ = fs.Parse(args)

	if *user == "" {
		return errors.New("не указан user")
	}

	if *id == 0 {
		return errors.New("не указан id")
	}

	info, err := r.client.Status(ctx, *user, *id)
	if err != nil {
		return err
	}

	fmt.Printf("meeting_id: %d\n", info.MeetingID)
	fmt.Printf("status: %s\n", string(info.Status))
	fmt.Printf("created_at: %s\n", info.CreatedAt.Format(time.RFC3339))
	fmt.Printf("updated_at: %s\n", info.UpdatedAt.Format(time.RFC3339))

	if info.ErrorMessage != "" {
		fmt.Printf("error: %s\n", info.ErrorMessage)
	}

	return nil
}

func (r *Runner) get(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("get", flag.ExitOnError)
	user := fs.String("user", r.defaultUser, "external user id")
	id := fs.Int64("id", 0, "meeting id")
	_ = fs.Parse(args)

	if *user == "" {
		return errors.New("не указан user")
	}

	if *id == 0 {
		return errors.New("не указан id")
	}

	content, err := r.client.GetTranscription(ctx, *user, *id)
	if err != nil {
		return err
	}

	fmt.Println(content)

	return nil
}

func (r *Runner) find(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("find", flag.ExitOnError)
	user := fs.String("user", r.defaultUser, "external user id")
	keyword := fs.String("keyword", "", "search keyword")
	_ = fs.Parse(args)

	if *user == "" {
		return errors.New("не указан user")
	}

	if *keyword == "" {
		return errors.New("не указан keyword")
	}

	items, err := r.client.Find(ctx, *user, *keyword)
	if err != nil {
		return err
	}

	if len(items) == 0 {
		fmt.Println("ничего не найдено")
		return nil
	}

	for _, item := range items {
		fmt.Printf(
			"%d\t%s\t%s\t%s\t%s\n",
			item.MeetingID,
			item.CreatedAt.Format(time.RFC3339),
			string(item.Status),
			item.Title,
			firstLine(item.Snippet, 80),
		)
	}

	return nil
}

func (r *Runner) chat(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("chat", flag.ExitOnError)
	user := fs.String("user", r.defaultUser, "external user id")
	id := fs.Int64("id", 0, "meeting id")
	text := fs.String("text", "", "question text")
	_ = fs.Parse(args)

	if *user == "" {
		return errors.New("не указан user")
	}

	if *id == 0 {
		return errors.New("не указан id")
	}

	if *text == "" {
		return errors.New("не указан text")
	}

	answer, err := r.client.Chat(ctx, *user, *id, *text)
	if err != nil {
		return err
	}

	fmt.Println(answer)

	return nil
}

func (r *Runner) retry(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("retry", flag.ExitOnError)
	user := fs.String("user", r.defaultUser, "external user id")
	id := fs.Int64("id", 0, "meeting id")
	_ = fs.Parse(args)

	if *user == "" {
		return errors.New("не указан user")
	}

	if *id == 0 {
		return errors.New("не указан id")
	}

	if err := r.client.Retry(ctx, *user, *id); err != nil {
		return err
	}

	fmt.Println("задача поставлена в повторную обработку")

	return nil
}

// firstLine обрезает строку до limit рун и добавляет многоточие, если строка длиннее.
func firstLine(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}

	return string(runes[:limit]) + "..."
}
