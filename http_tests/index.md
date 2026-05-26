已创建 8 个分类 HTTP Client 测试文件，覆盖全部接口：

文件	分类	接口数
http_tests/ai-service.http	AI 服务接口	/v1/models, /v1/chat/completions, /v1/messages
http_tests/system-status.http	系统状态管理	/admin/status, /admin/overview, /admin/dashboard
http_tests/account.http	账户管理	/admin/account, refresh/test/bootstrap/status/submit
http_tests/session.http	会话管理	列表 + 删除
http_tests/settings.http	设置管理	GET/PUT
http_tests/models.http	模型管理	列表 + 刷新
http_tests/logs.http	日志管理	列表/详情/重放/清理/导出 + 统计导出
http_tests/policies.http	策略管理	CRUD + 测试匹配

每个文件顶部有 @baseUrl 和 @adminToken 变量定义，使用前修改 @adminToken 为实际的 admin token 即可在 GoLand 中直接运行。原有的 test.http 未改动，保留用于向后兼容。