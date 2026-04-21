# Integration Matrix Results

Generated: 2026-04-20T02:29:21+02:00

| Target | Scenario | Status | Service | Provider | API | Caching | Reason |
|---|---|---|---|---|---|---|---|
| anthropic_api_sonnet | cache_explicit_wire_marked | ✅ | anthropic | anthropic | anthropic-messages | mode=explicit,configurable=yes,request=top_level_cache_control,message=block_cache_control,precedence=yes,suppress_request=yes,source=fallback | - |
| anthropic_api_sonnet | cache_message_overrides_request_level | ✅ | anthropic | anthropic | anthropic-messages | mode=explicit,configurable=yes,request=top_level_cache_control,message=block_cache_control,precedence=yes,suppress_request=yes,source=fallback | - |
| anthropic_api_sonnet | cache_not_sent_by_default | ✅ | anthropic | anthropic | anthropic-messages | mode=explicit,configurable=yes,request=top_level_cache_control,message=block_cache_control,precedence=yes,suppress_request=yes,source=fallback | - |
| anthropic_api_sonnet | cache_usage_effective | ✅ | anthropic | anthropic | anthropic-messages | mode=explicit,configurable=yes,request=top_level_cache_control,message=block_cache_control,precedence=yes,suppress_request=yes,source=fallback | - |
| anthropic_api_sonnet | effort_high_preserved | ⏭️ | - | - | - | mode=explicit,configurable=yes,request=top_level_cache_control,message=block_cache_control,precedence=yes,suppress_request=yes,source=fallback | target does not advertise effort support |
| anthropic_api_sonnet | plain_text_pong | ✅ | anthropic | anthropic | anthropic-messages | mode=explicit,configurable=yes,request=top_level_cache_control,message=block_cache_control,precedence=yes,suppress_request=yes,source=fallback | - |
| anthropic_api_sonnet | system_prompt_kiwi | ✅ | anthropic | anthropic | anthropic-messages | mode=explicit,configurable=yes,request=top_level_cache_control,message=block_cache_control,precedence=yes,suppress_request=yes,source=fallback | - |
| anthropic_api_sonnet | thinking_off_respected | ⏭️ | - | - | - | mode=explicit,configurable=yes,request=top_level_cache_control,message=block_cache_control,precedence=yes,suppress_request=yes,source=fallback | target does not advertise thinking toggle support |
| anthropic_api_sonnet | thinking_text_comet | ⏭️ | - | - | - | mode=explicit,configurable=yes,request=top_level_cache_control,message=block_cache_control,precedence=yes,suppress_request=yes,source=fallback | target does not advertise reasoning support |
| claude_sonnet | cache_explicit_wire_marked | ✅ | claude | local | anthropic-messages | mode=explicit,configurable=yes,request=top_level_cache_control,message=block_cache_control,precedence=yes,suppress_request=yes,source=fallback | - |
| claude_sonnet | cache_message_overrides_request_level | ✅ | claude | local | anthropic-messages | mode=explicit,configurable=yes,request=top_level_cache_control,message=block_cache_control,precedence=yes,suppress_request=yes,source=fallback | - |
| claude_sonnet | cache_not_sent_by_default | ✅ | claude | local | anthropic-messages | mode=explicit,configurable=yes,request=top_level_cache_control,message=block_cache_control,precedence=yes,suppress_request=yes,source=fallback | - |
| claude_sonnet | cache_usage_effective | ⏭️ | - | - | - | mode=explicit,configurable=yes,request=top_level_cache_control,message=block_cache_control,precedence=yes,suppress_request=yes,source=fallback | local Claude runtime does not reliably report cache-read usage |
| claude_sonnet | effort_high_preserved | ⏭️ | - | - | - | mode=explicit,configurable=yes,request=top_level_cache_control,message=block_cache_control,precedence=yes,suppress_request=yes,source=fallback | target does not advertise effort support |
| claude_sonnet | plain_text_pong | ✅ | claude | local | anthropic-messages | mode=explicit,configurable=yes,request=top_level_cache_control,message=block_cache_control,precedence=yes,suppress_request=yes,source=fallback | - |
| claude_sonnet | system_prompt_kiwi | ✅ | claude | local | anthropic-messages | mode=explicit,configurable=yes,request=top_level_cache_control,message=block_cache_control,precedence=yes,suppress_request=yes,source=fallback | - |
| claude_sonnet | thinking_off_respected | ⏭️ | - | - | - | mode=explicit,configurable=yes,request=top_level_cache_control,message=block_cache_control,precedence=yes,suppress_request=yes,source=fallback | target does not advertise thinking toggle support |
| claude_sonnet | thinking_text_comet | ⏭️ | - | - | - | mode=explicit,configurable=yes,request=top_level_cache_control,message=block_cache_control,precedence=yes,suppress_request=yes,source=fallback | target does not advertise reasoning support |
| codex_gpt54 | cache_explicit_wire_marked | ⏭️ | - | - | - | mode=implicit,mode=implicit,source=modeldb | target does not advertise explicit caching controls |
| codex_gpt54 | cache_message_overrides_request_level | ⏭️ | - | - | - | mode=implicit,mode=implicit,source=modeldb | message/request cache precedence is not meaningful for this exposure |
| codex_gpt54 | cache_not_sent_by_default | ✅ | codex | codex | openai-responses | mode=implicit,mode=implicit,source=modeldb | - |
| codex_gpt54 | cache_usage_effective | ⏭️ | codex | codex | openai-responses | mode=implicit,mode=implicit,source=modeldb | - |
| codex_gpt54 | effort_high_preserved | ✅ | codex | codex | openai-responses | mode=implicit,mode=implicit,source=modeldb | - |
| codex_gpt54 | plain_text_pong | ✅ | codex | codex | openai-responses | mode=implicit,mode=implicit,source=modeldb | - |
| codex_gpt54 | system_prompt_kiwi | ✅ | codex | codex | openai-responses | mode=implicit,mode=implicit,source=modeldb | - |
| codex_gpt54 | thinking_off_respected | ✅ | codex | codex | openai-responses | mode=implicit,mode=implicit,source=modeldb | - |
| codex_gpt54 | thinking_text_comet | ✅ | codex | codex | openai-responses | mode=implicit,mode=implicit,source=modeldb | - |
| minimax_m27 | cache_explicit_wire_marked | ✅ | minimax | minimax | anthropic-messages | mode=explicit,configurable=yes,request=top_level_cache_control,source=fallback | - |
| minimax_m27 | cache_message_overrides_request_level | ⏭️ | - | - | - | mode=explicit,configurable=yes,request=top_level_cache_control,source=fallback | message/request cache precedence is not meaningful for this exposure |
| minimax_m27 | cache_not_sent_by_default | ✅ | minimax | minimax | anthropic-messages | mode=explicit,configurable=yes,request=top_level_cache_control,source=fallback | - |
| minimax_m27 | cache_usage_effective | ✅ | minimax | minimax | anthropic-messages | mode=explicit,configurable=yes,request=top_level_cache_control,source=fallback | - |
| minimax_m27 | effort_high_preserved | ⏭️ | - | - | - | mode=explicit,configurable=yes,request=top_level_cache_control,source=fallback | target does not advertise effort support |
| minimax_m27 | plain_text_pong | ✅ | minimax | minimax | anthropic-messages | mode=explicit,configurable=yes,request=top_level_cache_control,source=fallback | - |
| minimax_m27 | system_prompt_kiwi | ✅ | minimax | minimax | anthropic-messages | mode=explicit,configurable=yes,request=top_level_cache_control,source=fallback | - |
| minimax_m27 | thinking_off_respected | ⏭️ | - | - | - | mode=explicit,configurable=yes,request=top_level_cache_control,source=fallback | target does not advertise thinking toggle support |
| minimax_m27 | thinking_text_comet | ⏭️ | - | - | - | mode=explicit,configurable=yes,request=top_level_cache_control,source=fallback | target does not advertise reasoning support |
| openai_gpt4o | cache_explicit_wire_marked | ⏭️ | - | - | - | none | target does not advertise caching support |
| openai_gpt4o | cache_message_overrides_request_level | ⏭️ | - | - | - | none | message/request cache precedence is not meaningful for this exposure |
| openai_gpt4o | cache_not_sent_by_default | ⏭️ | - | - | - | none | target does not advertise caching support |
| openai_gpt4o | cache_usage_effective | ⏭️ | - | - | - | none | target does not advertise caching support |
| openai_gpt4o | effort_high_preserved | ⏭️ | - | - | - | none | target does not advertise effort support |
| openai_gpt4o | plain_text_pong | ✅ | openai | openai | openai-chat | none | - |
| openai_gpt4o | system_prompt_kiwi | ✅ | openai | openai | openai-chat | none | - |
| openai_gpt4o | thinking_off_respected | ⏭️ | - | - | - | none | target does not advertise thinking toggle support |
| openai_gpt4o | thinking_text_comet | ⏭️ | - | - | - | none | target does not advertise reasoning support |
| openai_gpt51 | cache_explicit_wire_marked | ✅ | openai | openai | openai-responses | mode=mixed,configurable=yes,request=prompt_cache_retention,source=modeldb | - |
| openai_gpt51 | cache_message_overrides_request_level | ⏭️ | - | - | - | mode=mixed,configurable=yes,request=prompt_cache_retention,source=modeldb | message/request cache precedence is not meaningful for this exposure |
| openai_gpt51 | cache_not_sent_by_default | ✅ | openai | openai | openai-responses | mode=mixed,configurable=yes,request=prompt_cache_retention,source=modeldb | - |
| openai_gpt51 | cache_usage_effective | ✅ | openai | openai | openai-responses | mode=mixed,configurable=yes,request=prompt_cache_retention,source=modeldb | - |
| openai_gpt51 | effort_high_preserved | ✅ | openai | openai | openai-responses | mode=mixed,configurable=yes,request=prompt_cache_retention,source=modeldb | - |
| openai_gpt51 | plain_text_pong | ✅ | openai | openai | openai-responses | mode=mixed,configurable=yes,request=prompt_cache_retention,source=modeldb | - |
| openai_gpt51 | system_prompt_kiwi | ✅ | openai | openai | openai-responses | mode=mixed,configurable=yes,request=prompt_cache_retention,source=modeldb | - |
| openai_gpt51 | thinking_off_respected | ✅ | openai | openai | openai-responses | mode=mixed,configurable=yes,request=prompt_cache_retention,source=modeldb | - |
| openai_gpt51 | thinking_text_comet | ✅ | openai | openai | openai-responses | mode=mixed,configurable=yes,request=prompt_cache_retention,source=modeldb | - |
| openai_gpt54 | cache_explicit_wire_marked | ✅ | openai | openai | openai-responses | mode=mixed,configurable=yes,request=prompt_cache_retention,source=modeldb | - |
| openai_gpt54 | cache_message_overrides_request_level | ⏭️ | - | - | - | mode=mixed,configurable=yes,request=prompt_cache_retention,source=modeldb | message/request cache precedence is not meaningful for this exposure |
| openai_gpt54 | cache_not_sent_by_default | ✅ | openai | openai | openai-responses | mode=mixed,configurable=yes,request=prompt_cache_retention,source=modeldb | - |
| openai_gpt54 | cache_usage_effective | ✅ | openai | openai | openai-responses | mode=mixed,configurable=yes,request=prompt_cache_retention,source=modeldb | - |
| openai_gpt54 | effort_high_preserved | ✅ | openai | openai | openai-responses | mode=mixed,configurable=yes,request=prompt_cache_retention,source=modeldb | - |
| openai_gpt54 | plain_text_pong | ✅ | openai | openai | openai-responses | mode=mixed,configurable=yes,request=prompt_cache_retention,source=modeldb | - |
| openai_gpt54 | system_prompt_kiwi | ✅ | openai | openai | openai-responses | mode=mixed,configurable=yes,request=prompt_cache_retention,source=modeldb | - |
| openai_gpt54 | thinking_off_respected | ✅ | openai | openai | openai-responses | mode=mixed,configurable=yes,request=prompt_cache_retention,source=modeldb | - |
| openai_gpt54 | thinking_text_comet | ✅ | openai | openai | openai-responses | mode=mixed,configurable=yes,request=prompt_cache_retention,source=modeldb | - |
| openrouter_openai_gpt4o_mini | cache_explicit_wire_marked | ✅ | openrouter | openrouter | openai-responses | mode=explicit,configurable=yes,request=prompt_cache_retention,source=fallback,note=openrouter heuristic | - |
| openrouter_openai_gpt4o_mini | cache_message_overrides_request_level | ⏭️ | - | - | - | mode=explicit,configurable=yes,request=prompt_cache_retention,source=fallback,note=openrouter heuristic | message/request cache precedence is not meaningful for this exposure |
| openrouter_openai_gpt4o_mini | cache_not_sent_by_default | ✅ | openrouter | openrouter | openai-responses | mode=explicit,configurable=yes,request=prompt_cache_retention,source=fallback,note=openrouter heuristic | - |
| openrouter_openai_gpt4o_mini | cache_usage_effective | ⏭️ | - | - | - | mode=explicit,configurable=yes,request=prompt_cache_retention,source=fallback,note=openrouter heuristic | target does not reliably report cache-read usage for this scenario |
| openrouter_openai_gpt4o_mini | effort_high_preserved | ⏭️ | - | - | - | mode=explicit,configurable=yes,request=prompt_cache_retention,source=fallback,note=openrouter heuristic | target does not advertise effort support |
| openrouter_openai_gpt4o_mini | plain_text_pong | ✅ | openrouter | openrouter | openai-responses | mode=explicit,configurable=yes,request=prompt_cache_retention,source=fallback,note=openrouter heuristic | - |
| openrouter_openai_gpt4o_mini | system_prompt_kiwi | ✅ | openrouter | openrouter | openai-responses | mode=explicit,configurable=yes,request=prompt_cache_retention,source=fallback,note=openrouter heuristic | - |
| openrouter_openai_gpt4o_mini | thinking_off_respected | ⏭️ | - | - | - | mode=explicit,configurable=yes,request=prompt_cache_retention,source=fallback,note=openrouter heuristic | target does not advertise thinking toggle support |
| openrouter_openai_gpt4o_mini | thinking_text_comet | ⏭️ | - | - | - | mode=explicit,configurable=yes,request=prompt_cache_retention,source=fallback,note=openrouter heuristic | target does not advertise reasoning support |
| openrouter_openai_gpt51 | cache_explicit_wire_marked | ✅ | openrouter | openrouter | openai-responses | mode=explicit,configurable=yes,request=prompt_cache_retention,source=fallback,note=openrouter heuristic | - |
| openrouter_openai_gpt51 | cache_message_overrides_request_level | ⏭️ | - | - | - | mode=explicit,configurable=yes,request=prompt_cache_retention,source=fallback,note=openrouter heuristic | message/request cache precedence is not meaningful for this exposure |
| openrouter_openai_gpt51 | cache_not_sent_by_default | ✅ | openrouter | openrouter | openai-responses | mode=explicit,configurable=yes,request=prompt_cache_retention,source=fallback,note=openrouter heuristic | - |
| openrouter_openai_gpt51 | cache_usage_effective | ✅ | openrouter | openrouter | openai-responses | mode=explicit,configurable=yes,request=prompt_cache_retention,source=fallback,note=openrouter heuristic | - |
| openrouter_openai_gpt51 | effort_high_preserved | ✅ | openrouter | openrouter | openai-responses | mode=explicit,configurable=yes,request=prompt_cache_retention,source=fallback,note=openrouter heuristic | - |
| openrouter_openai_gpt51 | plain_text_pong | ✅ | openrouter | openrouter | openai-responses | mode=explicit,configurable=yes,request=prompt_cache_retention,source=fallback,note=openrouter heuristic | - |
| openrouter_openai_gpt51 | system_prompt_kiwi | ✅ | openrouter | openrouter | openai-responses | mode=explicit,configurable=yes,request=prompt_cache_retention,source=fallback,note=openrouter heuristic | - |
| openrouter_openai_gpt51 | thinking_off_respected | ✅ | openrouter | openrouter | openai-responses | mode=explicit,configurable=yes,request=prompt_cache_retention,source=fallback,note=openrouter heuristic | - |
| openrouter_openai_gpt51 | thinking_text_comet | ✅ | openrouter | openrouter | openai-responses | mode=explicit,configurable=yes,request=prompt_cache_retention,source=fallback,note=openrouter heuristic | - |
| openrouter_openai_gpt54 | cache_explicit_wire_marked | ✅ | openrouter | openrouter | openai-responses | mode=explicit,configurable=yes,request=prompt_cache_retention,source=fallback,note=openrouter heuristic | - |
| openrouter_openai_gpt54 | cache_message_overrides_request_level | ⏭️ | - | - | - | mode=explicit,configurable=yes,request=prompt_cache_retention,source=fallback,note=openrouter heuristic | message/request cache precedence is not meaningful for this exposure |
| openrouter_openai_gpt54 | cache_not_sent_by_default | ✅ | openrouter | openrouter | openai-responses | mode=explicit,configurable=yes,request=prompt_cache_retention,source=fallback,note=openrouter heuristic | - |
| openrouter_openai_gpt54 | cache_usage_effective | ✅ | openrouter | openrouter | openai-responses | mode=explicit,configurable=yes,request=prompt_cache_retention,source=fallback,note=openrouter heuristic | - |
| openrouter_openai_gpt54 | effort_high_preserved | ✅ | openrouter | openrouter | openai-responses | mode=explicit,configurable=yes,request=prompt_cache_retention,source=fallback,note=openrouter heuristic | - |
| openrouter_openai_gpt54 | plain_text_pong | ✅ | openrouter | openrouter | openai-responses | mode=explicit,configurable=yes,request=prompt_cache_retention,source=fallback,note=openrouter heuristic | - |
| openrouter_openai_gpt54 | system_prompt_kiwi | ✅ | openrouter | openrouter | openai-responses | mode=explicit,configurable=yes,request=prompt_cache_retention,source=fallback,note=openrouter heuristic | - |
| openrouter_openai_gpt54 | thinking_off_respected | ✅ | openrouter | openrouter | openai-responses | mode=explicit,configurable=yes,request=prompt_cache_retention,source=fallback,note=openrouter heuristic | - |
| openrouter_openai_gpt54 | thinking_text_comet | ❌ | - | - | - | mode=explicit,configurable=yes,request=prompt_cache_retention,source=fallback,note=openrouter heuristic | - |
