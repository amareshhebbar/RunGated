PII = "Please email carol.customer@clientmail.example about INV-000123"
EMAIL = "carol.customer@clientmail.example"


def test_openai_wrapper(ab, gateway):
    from openai import OpenAI

    _, up = gateway
    c = ab.wrap_openai(OpenAI(api_key="unused", base_url="http://unused.invalid/v1"))
    r = c.chat.completions.create(model="mock-1", messages=[{"role": "user", "content": PII}])
    m = r.choices[0].message
    assert EMAIL in m.content
    assert [t.function.name for t in m.tool_calls] == ["get_invoice"]
    assert "carol.customer" not in up.seen[-1][1]


def test_openai_factory_and_async(ab, gateway):
    c = ab.openai()
    r = c.chat.completions.create(model="mock-1", messages=[{"role": "user", "content": PII}])
    assert EMAIL in r.choices[0].message.content


async def test_async_openai_factory(ab):
    c = ab.async_openai()
    r = await c.chat.completions.create(model="mock-1", messages=[{"role": "user", "content": PII}])
    assert EMAIL in r.choices[0].message.content


def test_anthropic_wrapper(ab, gateway):
    from anthropic import Anthropic

    _, up = gateway
    c = ab.wrap_anthropic(Anthropic(api_key="unused", base_url="http://unused.invalid"))
    r = c.messages.create(model="claude-sonnet-4", max_tokens=50, messages=[{"role": "user", "content": PII}])
    assert [b.type for b in r.content] == ["text", "tool_use", "text"]
    assert EMAIL in r.content[0].text
    assert "carol.customer" not in up.seen[-1][1]


def test_anthropic_factory(ab):
    c = ab.anthropic()
    r = c.messages.create(model="claude-sonnet-4", max_tokens=50, messages=[{"role": "user", "content": PII}])
    assert EMAIL in r.content[0].text


def test_blocked_model_raises(ab):
    import pytest
    from openai import APIStatusError

    c = ab.openai()
    with pytest.raises(APIStatusError) as e:
        c.chat.completions.create(model="forbidden-model", messages=[{"role": "user", "content": "hi"}])
    assert e.value.status_code == 403
