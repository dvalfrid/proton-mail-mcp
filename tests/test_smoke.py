def test_server_imports():
    from proton_mail_mcp import server

    assert server.mcp.name == "proton-mail"
