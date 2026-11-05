-- 0020_drop_ai_recommendations: AI Mode was removed, so nothing reads or writes
-- this table any more. Claude now gets its recommendations through the MCP
-- server instead (internal/mcpserver).
DROP TABLE ai_recommendations;
