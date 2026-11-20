ALTER TABLE degradation_monitors DROP CONSTRAINT IF EXISTS degradation_monitors_protocol_check;
ALTER TABLE degradation_monitors ADD CONSTRAINT degradation_monitors_protocol_check
    CHECK (protocol IN ('openai_chat', 'openai_responses', 'anthropic', 'gemini_native'));
