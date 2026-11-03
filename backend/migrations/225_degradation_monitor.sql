-- 降智检测（degradation monitor）：管理员按分组定时让模型生成同一道 SVG/HTML
-- 动画题，保存最近的结果供用户比较模型表现。检测走站点自身网关与托管内部密钥，
-- 不扣管理员余额，消耗计入运营面板的监控消费。

-- 预设提示词。内置题目来自 codex2api 质量测试题库，可编辑、可删除。
CREATE TABLE IF NOT EXISTS degradation_prompts (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    prompt TEXT NOT NULL,
    builtin_key VARCHAR(32) NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS degradation_prompts_builtin_key_idx
    ON degradation_prompts (builtin_key) WHERE builtin_key <> '';

-- 检测分组：一个站点分组 + 模型 + 请求协议构成一条定时检测。
-- interval_minutes / reasoning_effort 为 0 / 空时跟随全局配置。
CREATE TABLE IF NOT EXISTS degradation_monitors (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL DEFAULT '',
    group_id BIGINT NOT NULL,
    model VARCHAR(200) NOT NULL,
    reasoning_effort VARCHAR(16) NOT NULL DEFAULT '',
    protocol VARCHAR(32) NOT NULL,
    interval_minutes INTEGER NOT NULL DEFAULT 0 CHECK (interval_minutes >= 0),
    prompt_id BIGINT REFERENCES degradation_prompts(id) ON DELETE SET NULL,
    random_prompt BOOLEAN NOT NULL DEFAULT FALSE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    api_key_id BIGINT NOT NULL DEFAULT 0,
    created_by BIGINT NOT NULL DEFAULT 0,
    last_run_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT degradation_monitors_protocol_check
        CHECK (protocol IN ('openai_chat', 'openai_responses', 'anthropic'))
);

CREATE INDEX IF NOT EXISTS degradation_monitors_sort_idx
    ON degradation_monitors (sort_order, id);

-- 检测结果。删除检测分组时级联删除其全部动画；每个分组只保留最近 100 条。
CREATE TABLE IF NOT EXISTS degradation_runs (
    id BIGSERIAL PRIMARY KEY,
    monitor_id BIGINT NOT NULL REFERENCES degradation_monitors(id) ON DELETE CASCADE,
    status VARCHAR(16) NOT NULL CHECK (status IN ('running', 'success', 'failed')),
    trigger_source VARCHAR(16) NOT NULL DEFAULT 'schedule',
    model VARCHAR(200) NOT NULL DEFAULT '',
    response_model VARCHAR(200) NOT NULL DEFAULT '',
    reasoning_effort VARCHAR(16) NOT NULL DEFAULT '',
    protocol VARCHAR(32) NOT NULL DEFAULT '',
    prompt_id BIGINT,
    prompt_name VARCHAR(100) NOT NULL DEFAULT '',
    prompt TEXT NOT NULL DEFAULT '',
    output TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    attempts INTEGER NOT NULL DEFAULT 0,
    first_token_ms INTEGER,
    duration_ms INTEGER,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    reasoning_tokens BIGINT NOT NULL DEFAULT 0,
    cost NUMERIC(20, 10),
    usage_synced BOOLEAN NOT NULL DEFAULT FALSE,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS degradation_runs_monitor_idx
    ON degradation_runs (monitor_id, id DESC);

CREATE INDEX IF NOT EXISTS degradation_runs_running_idx
    ON degradation_runs (started_at) WHERE status = 'running';

INSERT INTO degradation_prompts (name, prompt, builtin_key, sort_order) VALUES
    ('鹈鹕骑自行车', '创建一个 HTML，内容是用 SVG 绘制一个鹈鹕骑自行车的 2D 动画。你不需要任何测试。', 'pelican', 10),
    ('北极熊骑自行车', '创建一个 HTML，用 SVG 绘制一只北极熊骑自行车的 2D 动画：北极熊体型圆润、白色毛发、黑鼻子，坐在一辆两轮自行车上，双腿随踏板做圆周蹬踏，车轮持续转动并带辐条，背景是雪地与远处的冰山，雪花缓缓飘落。所有样式与脚本内联。你不需要任何测试。', 'polarbear', 20),
    ('奥特曼骑摩托车', '创建一个 HTML，用 SVG 绘制奥特曼骑摩托车的 2D 动画：奥特曼为银红配色、头顶有鳍状冠、胸口有闪烁的计时器，跨坐在一辆流线型摩托车上向右疾驰，车轮高速旋转、尾部拖出速度线，背景是横向滚动的城市楼群，天上偶尔有怪兽剪影掠过。所有样式与脚本内联。你不需要任何测试。', 'ultraman', 30),
    ('章鱼打架子鼓', '创建一个 HTML，用 SVG 绘制一只章鱼打架子鼓的 2D 动画：章鱼有 8 条可辨的触手，分别握着鼓棒，同时敲击底鼓、军鼓、镲片等至少 5 件鼓组部件，敲击节奏要稳定循环，被击中的部件有震动或闪光反馈，章鱼头部随节拍轻微点动。所有样式与脚本内联。你不需要任何测试。', 'octopus', 40),
    ('长颈鹿踩滑板', '创建一个 HTML，用 SVG 绘制一只长颈鹿踩滑板的 2D 动画：长颈鹿脖子明显修长、带棕色斑块，四蹄站在一块滑板上从左向右滑行，滑板轮子转动，途中遇到一个小坡道时做起跳与落地动作，脖子随动作前后摆动，背景是热带草原与移动的云。所有样式与脚本内联。你不需要任何测试。', 'giraffe', 50),
    ('宇航猫月球钓鱼', '创建一个 HTML，用 SVG 绘制一只穿宇航服的猫在月球上钓鱼的 2D 动画：猫戴着透明头盔、尾巴从宇航服后面伸出，坐在月面环形山边缘，鱼竿伸向一片漂浮的星星湖，钓线末端不时钓起一颗闪光的星星，背景是漆黑太空、缓慢自转的地球与散布的星点，猫会偶尔甩尾。所有样式与脚本内联。你不需要任何测试。', 'mooncat', 60),
    ('模拟时钟 · 实时走针', '创建一个 HTML，用 SVG 绘制一个模拟时钟：有表盘刻度、时针、分针、秒针，读取浏览器本地时间实时走针，秒针平滑扫动，时针随分钟联动偏移。表盘下方用数字显示当前时间。所有样式与脚本内联，不引用外部资源。你不需要任何测试。', 'clock', 70),
    ('太阳系轨道动画', '创建一个 HTML，用 SVG 绘制太阳系动画：太阳居中，至少包含水星、金星、地球、火星、木星、土星六颗行星沿各自轨道公转，公转周期按真实比例缩放（地球 1 圈时水星约 4 圈、土星约 1/29 圈），地球带一颗绕转的月球，土星有光环，每颗行星标注中文名称。深色星空背景带闪烁星点。所有样式与脚本内联。你不需要任何测试。', 'solar', 80),
    ('弹跳小球物理模拟', '创建一个 HTML，用 Canvas 或 SVG 实现弹跳小球物理模拟：初始生成 8 个不同颜色和半径的小球，受重力作用下落，碰到容器四壁反弹并有能量损耗，小球之间发生碰撞时按动量守恒交换速度，不允许相互穿透或卡在墙内。点击画面任意位置新增一个小球。所有样式与脚本内联。你不需要任何测试。', 'balls', 90),
    ('齿轮传动机构', '创建一个 HTML，用 SVG 绘制一组相互啮合的齿轮传动动画：至少 4 个齿轮，齿数分别为 12、24、36、18，齿形要真实可辨，相邻齿轮的齿必须正确啮合而不重叠或穿透，转速与齿数成反比，转向相邻相反。用不同颜色区分齿轮，并在每个齿轮中心标注齿数。所有样式与脚本内联。你不需要任何测试。', 'gears', 100),
    ('城市夜景视差滚动', '创建一个 HTML，用 SVG 绘制一幅横向无限滚动的城市夜景：至少三层视差（远景山脉与月亮、中景楼群、近景马路），层速由远到近递增；楼窗随机点亮熄灭，马路上有汽车往返行驶并带车灯，天空偶尔划过流星。画面循环时不能出现接缝或跳变。所有样式与脚本内联。你不需要任何测试。', 'city', 110),
    ('汉字笔顺书写动画', '创建一个 HTML，用 SVG 路径动画演示汉字「永」的书写过程：按正确笔顺逐笔书写，共 5 笔（点、横折钩、横撇、撇、捺），每一笔用 stroke-dashoffset 动画呈现毛笔运笔效果，笔画粗细有提按变化，写完一笔再写下一笔，全部写完后暂停两秒重新开始。旁边列出笔顺编号。所有样式与脚本内联。你不需要任何测试。', 'strokes', 120)
ON CONFLICT (builtin_key) WHERE builtin_key <> '' DO NOTHING;
