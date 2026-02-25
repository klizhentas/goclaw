package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/klizhentas/goclaw/internal/config"
	"github.com/klizhentas/goclaw/internal/conversation"
	"github.com/klizhentas/goclaw/internal/listener"
	"github.com/klizhentas/goclaw/internal/model"
	"github.com/klizhentas/goclaw/internal/outbound"
	"github.com/klizhentas/goclaw/internal/policy"
	"github.com/klizhentas/goclaw/internal/prompt"
	"github.com/klizhentas/goclaw/internal/store"
	"github.com/klizhentas/goclaw/internal/types"
	"github.com/klizhentas/goclaw/pkg/storage"
)

type App struct {
	cfg      config.Config
	logger   *slog.Logger
	store    storage.Store
	locks    *conversation.LockMap
	sem      chan struct{}
	model    model.Client
	listener *listener.CLIListener
	outbound *outbound.CLIOutbound
}

func New(cfg config.Config, logger *slog.Logger) (*App, error) {
	st, err := store.NewSQLiteStore(cfg.DatabasePath)
	if err != nil {
		return nil, err
	}
	modelClient, err := model.NewClient(cfg)
	if err != nil {
		_ = st.Close()
		return nil, err
	}

	return &App{
		cfg:      cfg,
		logger:   logger,
		store:    st,
		locks:    conversation.NewLockMap(),
		sem:      make(chan struct{}, cfg.MaxActiveConversations),
		model:    modelClient,
		listener: listener.NewCLIListener(os.Stdin, os.Stdout),
		outbound: outbound.NewCLIOutbound(os.Stdout),
	}, nil
}

func (a *App) Close() error {
	if a.store == nil {
		return nil
	}
	return a.store.Close()
}

func (a *App) Run(ctx context.Context, mode string) error {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "sender":
		return a.RunSender(ctx)
	case "worker":
		return a.RunWorker(ctx)
	case "scheduler":
		return a.RunScheduler(ctx)
	case "single", "":
		return a.RunSingle(ctx)
	default:
		return fmt.Errorf("unsupported mode %q (use sender|worker|scheduler|single)", mode)
	}
}

func (a *App) RunSingle(ctx context.Context) error {
	err := a.listener.Run(ctx, a.handleImmediate)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func (a *App) RunSender(ctx context.Context) error {
	senderCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		a.runSenderOutboundLoop(senderCtx)
	}()

	err := a.listener.Run(senderCtx, a.enqueueInbound)
	cancel()
	wg.Wait()
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func (a *App) RunWorker(ctx context.Context) error {
	a.runModelStartupDiagnostics(ctx)

	ticker := time.NewTicker(a.cfg.QueuePollInterval)
	defer ticker.Stop()

	var wg sync.WaitGroup
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return nil
		case <-ticker.C:
		}

		select {
		case a.sem <- struct{}{}:
		default:
			continue
		}

		item, err := a.store.ClaimNextInbound(ctx, a.cfg.WorkerID)
		if err != nil {
			<-a.sem
			a.logger.Error("claim queue message", "conversation_id", "", "message_id", "", "stage", "ingress", "error", err)
			continue
		}
		if item == nil {
			<-a.sem
			continue
		}

		wg.Add(1)
		go func(msg types.QueueMessage) {
			defer wg.Done()
			defer func() { <-a.sem }()
			if err := a.processQueuedMessage(ctx, msg); err != nil {
				a.logger.Error("worker message failed", "conversation_id", msg.ConversationID, "message_id", msg.ID, "stage", "ingress", "error", err)
			}
		}(*item)
	}
}

func (a *App) RunScheduler(ctx context.Context) error {
	ticker := time.NewTicker(a.cfg.QueuePollInterval)
	defer ticker.Stop()

	schedulerSem := make(chan struct{}, a.cfg.SchedulerConcurrency)
	var wg sync.WaitGroup

	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return nil
		case <-ticker.C:
		}

		select {
		case schedulerSem <- struct{}{}:
		default:
			continue
		}

		task, err := a.store.ClaimDueTask(ctx, a.cfg.SchedulerID, a.cfg.TaskLeaseDuration)
		if err != nil {
			<-schedulerSem
			a.logger.Error("claim due task", "conversation_id", "", "message_id", "", "stage", "ingress", "error", err)
			continue
		}
		if task == nil {
			<-schedulerSem
			continue
		}

		wg.Add(1)
		go func(t types.Task) {
			defer wg.Done()
			defer func() { <-schedulerSem }()
			if err := a.dispatchTaskRun(ctx, t); err != nil {
				a.logger.Error("dispatch task run failed", "conversation_id", t.ConversationID, "message_id", t.ID, "stage", "persist_in", "error", err)
			}
		}(*task)
	}
}

func (a *App) runModelStartupDiagnostics(ctx context.Context) {
	diag, ok := a.model.(model.DiagnosticClient)
	if !ok {
		a.logger.Info("model startup diagnostics skipped", "conversation_id", "", "message_id", "", "stage", "model_stream", "backend", a.cfg.ModelBackend)
		return
	}

	checkCtx, cancel := context.WithTimeout(ctx, a.cfg.RequestTimeout)
	defer cancel()

	info, err := diag.StartupCheck(checkCtx)
	if err != nil {
		a.logger.Error("model startup diagnostics failed", "conversation_id", "", "message_id", "", "stage", "model_stream", "error", err, "details", info)
		return
	}
	a.logger.Info("model startup diagnostics", "conversation_id", "", "message_id", "", "stage", "model_stream", "details", info)
}

func (a *App) enqueueInbound(ctx context.Context, in listener.InboundMessage) error {
	start := time.Now()
	a.logger.Info("stage start", "conversation_id", in.ConversationID, "message_id", "", "stage", "ingress")
	defer func() {
		a.logger.Info("stage done", "conversation_id", in.ConversationID, "message_id", "", "stage", "ingress", "duration_ms", time.Since(start).Milliseconds())
	}()

	persistStart := time.Now()
	a.logger.Info("stage start", "conversation_id", in.ConversationID, "message_id", "", "stage", "persist_in")
	queued, err := a.store.EnqueueInbound(ctx, in.ConversationID, in.Content)
	if err != nil {
		a.logger.Error("stage error", "conversation_id", in.ConversationID, "message_id", "", "stage", "persist_in", "duration_ms", time.Since(persistStart).Milliseconds(), "error", err)
		return err
	}
	a.logger.Info("stage done", "conversation_id", in.ConversationID, "message_id", queued.ID, "stage", "persist_in", "duration_ms", time.Since(persistStart).Milliseconds())
	return nil
}

func (a *App) handleImmediate(ctx context.Context, in listener.InboundMessage) error {
	queued, err := a.store.EnqueueInbound(ctx, in.ConversationID, in.Content)
	if err != nil {
		return err
	}
	return a.processQueuedMessage(ctx, queued)
}

func (a *App) processQueuedMessage(ctx context.Context, item types.QueueMessage) error {
	ingressStart := time.Now()
	a.logger.Info("stage start", "conversation_id", item.ConversationID, "message_id", item.ID, "stage", "ingress")
	defer func() {
		a.logger.Info("stage done", "conversation_id", item.ConversationID, "message_id", item.ID, "stage", "ingress", "duration_ms", time.Since(ingressStart).Milliseconds())
	}()

	unlock := a.locks.Lock(item.ConversationID)
	defer unlock()

	role := types.ConversationRoleNonMain
	if types.IsMainConversation(item.ConversationID, a.cfg.MainConversationID) {
		role = types.ConversationRoleMain
	}
	if !policy.ShouldProcess(a.cfg, item.ConversationID, item.Content) {
		a.logger.Info("message skipped by trigger policy", "conversation_id", item.ConversationID, "message_id", item.ID, "stage", "ingress")
		if item.RelatedMessageID != "" {
			_ = a.store.CompleteTaskRunFailure(ctx, item.RelatedMessageID, "message skipped by trigger policy")
		}
		return a.store.MarkQueueDone(ctx, item.ID)
	}

	persistInStart := time.Now()
	a.logger.Info("stage start", "conversation_id", item.ConversationID, "message_id", item.ID, "stage", "persist_in")
	if err := a.store.CreateConversationIfMissing(ctx, item.ConversationID, role); err != nil {
		a.markQueueError(ctx, item.ID, err)
		a.failLinkedTaskRun(ctx, item.RelatedMessageID, err)
		a.logger.Error("stage error", "conversation_id", item.ConversationID, "message_id", item.ID, "stage", "persist_in", "duration_ms", time.Since(persistInStart).Milliseconds(), "error", err)
		return err
	}
	userMsg, err := a.store.AppendMessage(ctx, item.ConversationID, types.MessageRoleUser, item.Content)
	if err != nil {
		a.markQueueError(ctx, item.ID, err)
		a.failLinkedTaskRun(ctx, item.RelatedMessageID, err)
		a.logger.Error("stage error", "conversation_id", item.ConversationID, "message_id", item.ID, "stage", "persist_in", "duration_ms", time.Since(persistInStart).Milliseconds(), "error", err)
		return err
	}
	a.logger.Info("stage done", "conversation_id", item.ConversationID, "message_id", userMsg.ID, "stage", "persist_in", "duration_ms", time.Since(persistInStart).Milliseconds())

	messages, err := a.store.GetRecentMessages(ctx, item.ConversationID, a.cfg.HistoryWindow)
	if err != nil {
		a.markQueueError(ctx, item.ID, err)
		a.failLinkedTaskRun(ctx, item.RelatedMessageID, err)
		return err
	}

	promptStart := time.Now()
	a.logger.Info("stage start", "conversation_id", item.ConversationID, "message_id", userMsg.ID, "stage", "prompt_build")
	system := prompt.BuildSystemPrompt(role, a.cfg.AssistantName)
	built := prompt.BuildMessages(system, messages, a.cfg.HistoryWindow, 4000)
	a.logger.Info("stage done", "conversation_id", item.ConversationID, "message_id", userMsg.ID, "stage", "prompt_build", "duration_ms", time.Since(promptStart).Milliseconds())

	modelStart := time.Now()
	a.logger.Info("stage start", "conversation_id", item.ConversationID, "message_id", userMsg.ID, "stage", "model_stream")
	respCtx, cancel := context.WithTimeout(ctx, a.cfg.RequestTimeout)
	defer cancel()

	var full strings.Builder
	response, err := a.model.StreamResponse(respCtx, built, func(token string) error {
		full.WriteString(token)
		return nil
	})
	if err != nil {
		a.markQueueError(ctx, item.ID, err)
		a.failLinkedTaskRun(ctx, item.RelatedMessageID, err)
		a.logger.Error("stage error", "conversation_id", item.ConversationID, "message_id", userMsg.ID, "stage", "model_stream", "duration_ms", time.Since(modelStart).Milliseconds(), "error", err)
		return fmt.Errorf("model stream: %w", err)
	}
	if response == "" {
		response = full.String()
	}
	a.logger.Info("stage done", "conversation_id", item.ConversationID, "message_id", userMsg.ID, "stage", "model_stream", "duration_ms", time.Since(modelStart).Milliseconds())

	persistOutStart := time.Now()
	a.logger.Info("stage start", "conversation_id", item.ConversationID, "message_id", userMsg.ID, "stage", "persist_out")
	assistantMsg, err := a.store.AppendMessage(ctx, item.ConversationID, types.MessageRoleAssistant, response)
	if err != nil {
		a.markQueueError(ctx, item.ID, err)
		a.failLinkedTaskRun(ctx, item.RelatedMessageID, err)
		a.logger.Error("stage error", "conversation_id", item.ConversationID, "message_id", userMsg.ID, "stage", "persist_out", "duration_ms", time.Since(persistOutStart).Milliseconds(), "error", err)
		return err
	}
	if _, err := a.store.EnqueueOutbound(ctx, item.ConversationID, response, assistantMsg.ID); err != nil {
		a.markQueueError(ctx, item.ID, err)
		a.failLinkedTaskRun(ctx, item.RelatedMessageID, err)
		return err
	}
	if err := a.store.MarkQueueDone(ctx, item.ID); err != nil {
		a.failLinkedTaskRun(ctx, item.RelatedMessageID, err)
		return err
	}
	if item.RelatedMessageID != "" {
		if err := a.store.CompleteTaskRunSuccess(ctx, item.RelatedMessageID, response, assistantMsg.ID); err != nil {
			a.logger.Error("complete linked task run success", "conversation_id", item.ConversationID, "message_id", item.RelatedMessageID, "stage", "persist_out", "error", err)
		}
	}
	a.logger.Info("stage done", "conversation_id", item.ConversationID, "message_id", assistantMsg.ID, "stage", "persist_out", "duration_ms", time.Since(persistOutStart).Milliseconds())

	egressStart := time.Now()
	a.logger.Info("stage start", "conversation_id", item.ConversationID, "message_id", assistantMsg.ID, "stage", "egress")
	a.outbound.Start(item.ConversationID)
	a.outbound.Token(response)
	a.outbound.End()
	a.logger.Info("stage done", "conversation_id", item.ConversationID, "message_id", assistantMsg.ID, "stage", "egress", "duration_ms", time.Since(egressStart).Milliseconds())

	return nil
}

func (a *App) markQueueError(ctx context.Context, queueID string, err error) {
	if markErr := a.store.MarkQueueError(ctx, queueID, err.Error()); markErr != nil {
		a.logger.Error("mark queue error", "conversation_id", "", "message_id", queueID, "stage", "persist_out", "error", markErr)
	}
}

func (a *App) runSenderOutboundLoop(ctx context.Context) {
	ticker := time.NewTicker(a.cfg.QueuePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		item, err := a.store.ClaimNextOutbound(ctx, a.cfg.SenderID)
		if err != nil {
			a.logger.Error("claim outbound message", "conversation_id", "", "message_id", "", "stage", "egress", "error", err)
			continue
		}
		if item == nil {
			continue
		}

		egressStart := time.Now()
		a.logger.Info("stage start", "conversation_id", item.ConversationID, "message_id", item.ID, "stage", "egress")
		a.outbound.Start(item.ConversationID)
		a.outbound.Token(item.Content)
		a.outbound.End()
		if err := a.store.MarkQueueDone(ctx, item.ID); err != nil {
			a.logger.Error("mark outbound done", "conversation_id", item.ConversationID, "message_id", item.ID, "stage", "egress", "error", err)
			continue
		}
		a.logger.Info("stage done", "conversation_id", item.ConversationID, "message_id", item.ID, "stage", "egress", "duration_ms", time.Since(egressStart).Milliseconds())
	}
}

func (a *App) dispatchTaskRun(ctx context.Context, task types.Task) error {
	run, err := a.store.CreateTaskRunQueued(ctx, task, a.cfg.SchedulerID)
	if err != nil {
		return err
	}
	msg, err := a.store.EnqueueInboundForTaskRun(ctx, task.ConversationID, task.Payload, run.ID)
	if err != nil {
		_ = a.store.CompleteTaskRunFailure(ctx, run.ID, err.Error())
		return err
	}
	if err := a.store.SetTaskRunInboundQueueMessageID(ctx, run.ID, msg.ID); err != nil {
		_ = a.store.CompleteTaskRunFailure(ctx, run.ID, err.Error())
		return err
	}
	if err := a.store.ReleaseTaskClaim(ctx, task.ID); err != nil {
		return err
	}
	return nil
}

func (a *App) failLinkedTaskRun(ctx context.Context, runID string, err error) {
	if runID == "" {
		return
	}
	if updateErr := a.store.CompleteTaskRunFailure(ctx, runID, err.Error()); updateErr != nil {
		a.logger.Error("complete linked task run failure", "conversation_id", "", "message_id", runID, "stage", "persist_out", "error", updateErr)
	}
}
