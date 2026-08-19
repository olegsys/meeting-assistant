DROP INDEX IF EXISTS idx_summaries_content_fts;
DROP INDEX IF EXISTS idx_transcriptions_content_fts;
DROP INDEX IF EXISTS idx_chat_messages_meeting_id;
DROP INDEX IF EXISTS idx_chat_messages_user_id;
DROP INDEX IF EXISTS idx_processing_tasks_updated_at;
DROP INDEX IF EXISTS idx_processing_tasks_status;
DROP INDEX IF EXISTS idx_meetings_user_id;

DROP TABLE IF EXISTS chat_messages;
DROP TABLE IF EXISTS summaries;
DROP TABLE IF EXISTS transcriptions;
DROP TABLE IF EXISTS processing_tasks;
DROP TABLE IF EXISTS meeting_files;
DROP TABLE IF EXISTS meetings;
DROP TABLE IF EXISTS users;