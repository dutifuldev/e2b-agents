package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/dutifuldev/e2b-agents/internal/app"
	"github.com/dutifuldev/e2b-agents/internal/config"
	"github.com/dutifuldev/e2b-agents/internal/database"
	"github.com/dutifuldev/e2b-agents/internal/gateway"
	"github.com/spf13/cobra"
)

func main() {
	configureLogger()
	if err := rootCommand().Execute(); err != nil {
		slog.Error("command failed", "error", err)
		os.Exit(1)
	}
}

func configureLogger() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
}

func rootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "e2b-agents",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.AddCommand(serveCommand())
	cmd.AddCommand(migrateCommand())
	cmd.AddCommand(devCommand())

	return cmd
}

func serveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the HTTP service",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load()
			if err := cfg.ValidateServe(); err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			runtime, err := app.NewRuntime(ctx, cfg)
			if err != nil {
				return err
			}
			defer runtime.Close()

			return runtime.Serve(ctx)
		},
	}
}

func migrateCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "migrate", Short: "Run database migrations"}
	cmd.AddCommand(&cobra.Command{
		Use:   "up",
		Short: "Apply migrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load()
			if err := cfg.ValidateDatabase(); err != nil {
				return err
			}
			db, err := database.Open(cfg.DatabaseURL, database.PoolConfig{
				MaxOpenConns: cfg.DatabaseMaxOpenConns,
				MaxIdleConns: cfg.DatabaseMaxIdleConns,
			})
			if err != nil {
				return err
			}
			return database.ApplyMigrations(cmd.Context(), db)
		},
	})
	return cmd
}

func devCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Development utilities",
	}

	cmd.AddCommand(devEnsureWorkspaceCommand())
	cmd.AddCommand(devSendCommand())
	cmd.AddCommand(devSlackAuthCommand())

	return cmd
}

func devEnsureWorkspaceCommand() *cobra.Command {
	var input gateway.EnsureWorkspaceInput
	cmd := &cobra.Command{
		Use:   "ensure-workspace",
		Short: "Create or update a Slack workspace mapping",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load()
			if err := cfg.ValidateDatabase(); err != nil {
				return err
			}
			db, err := database.Open(cfg.DatabaseURL, database.PoolConfig{
				MaxOpenConns: cfg.DatabaseMaxOpenConns,
				MaxIdleConns: cfg.DatabaseMaxIdleConns,
			})
			if err != nil {
				return err
			}
			service := gateway.NewWorkspaceService(db)
			workspace, err := service.EnsureWorkspace(cmd.Context(), input)
			if err != nil {
				return err
			}
			fmt.Printf("workspace_id=%s slack_team_id=%s team_id=%s template_id=%s\n", workspace.ID, workspace.SlackTeamID, workspace.TeamID, workspace.TemplateID)
			return nil
		},
	}
	cmd.Flags().StringVar(&input.SlackTeamID, "slack-team-id", "", "Slack team ID")
	cmd.Flags().StringVar(&input.SlackEnterpriseID, "slack-enterprise-id", "", "Slack enterprise ID")
	cmd.Flags().StringVar(&input.SlackTeamName, "slack-team-name", "", "Slack team name")
	cmd.Flags().StringVar(&input.TeamID, "team-id", "", "Owning team ID")
	cmd.Flags().StringVar(&input.TemplateID, "template-id", "", "E2B template ID or alias")
	cmd.Flags().StringVar(&input.BotTokenRef, "bot-token-ref", "env:SLACK_BOT_TOKEN", "Bot token reference")
	cmd.Flags().StringVar(&input.SigningSecretRef, "signing-secret-ref", "env:SLACK_SIGNING_SECRET", "Signing secret reference")
	cmd.Flags().StringVar(&input.BotUserID, "bot-user-id", "", "Slack bot user ID")
	_ = cmd.MarkFlagRequired("slack-team-id")
	_ = cmd.MarkFlagRequired("team-id")
	_ = cmd.MarkFlagRequired("template-id")
	return cmd
}

func devSendCommand() *cobra.Command {
	var slackTeamID string
	var channelID string
	var userID string
	var text string
	var postToSlack bool
	cmd := &cobra.Command{
		Use:   "send",
		Short: "Send one message through the gateway without a Slack event",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load()
			if err := cfg.ValidateDevSend(postToSlack); err != nil {
				return err
			}
			runtime, err := app.NewRuntime(context.Background(), cfg)
			if err != nil {
				return err
			}
			defer runtime.Close()

			reply, err := runtime.Gateway().HandleDirectMessage(cmd.Context(), gateway.DirectMessageInput{
				SlackTeamID: slackTeamID,
				ChannelID:   channelID,
				UserID:      userID,
				Text:        text,
				PostToSlack: postToSlack,
			})
			if err != nil {
				return err
			}
			fmt.Println(strings.TrimSpace(reply.Text))
			return nil
		},
	}
	cmd.Flags().StringVar(&slackTeamID, "slack-team-id", "", "Slack team ID")
	cmd.Flags().StringVar(&channelID, "channel-id", "", "Slack channel ID")
	cmd.Flags().StringVar(&userID, "user-id", "dev-user", "Requester user ID")
	cmd.Flags().StringVar(&text, "text", "", "Message text")
	cmd.Flags().BoolVar(&postToSlack, "post-to-slack", false, "Post the reply to Slack")
	_ = cmd.MarkFlagRequired("slack-team-id")
	_ = cmd.MarkFlagRequired("channel-id")
	_ = cmd.MarkFlagRequired("text")
	return cmd
}

func devSlackAuthCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "slack-auth",
		Short: "Print non-secret Slack auth test metadata",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load()
			client := gateway.NewSlackClient(cfg.SlackBotToken)
			info, err := client.AuthTest(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Printf("team_id=%s team=%s user_id=%s bot_id=%s\n", info.TeamID, info.Team, info.UserID, info.BotID)
			return nil
		},
	}
}
