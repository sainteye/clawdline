// Package productcopy holds the daemon's fixed, person-facing notification copy.
// Effect producers render this copy before recording an outbox intent, so a
// retry sends the same words even if the setting changes meanwhile.
package productcopy

import (
	"fmt"
	"log"
	"regexp"
	"strings"
)

// Languages is the order used by the notification catalog below.
var Languages = []string{"en", "zh-Hant", "ja", "zh-Hans", "ko", "es", "pt-BR", "fr", "de"}

// The first multilingual release carries all nine entries for each key.
// Later secondary-language additions may be empty: Format falls back on that
// key alone, and Coverage reports the untranslated count.
var notices = map[string][9]string{
	"waiting.title":             {"Waiting for your answer: {label}", "等你回答：{label}", "回答を待っています：{label}", "等待你的回答：{label}", "답변을 기다리는 중: {label}", "Esperando tu respuesta: {label}", "Aguardando sua resposta: {label}", "En attente de votre réponse : {label}", "Wartet auf Ihre Antwort: {label}"},
	"waiting.body":              {"Waiting on a question for {minutes} minutes", "停在一個問題上 {minutes} 分鐘了", "質問への回答を {minutes} 分間待っています", "等待问题的回答已 {minutes} 分钟", "질문에 대한 답변을 {minutes}분째 기다리고 있습니다", "Lleva {minutes} minutos esperando una respuesta", "Aguardando uma resposta há {minutes} minutos", "En attente d’une réponse depuis {minutes} minutes", "Wartet seit {minutes} Minuten auf eine Antwort"},
	"waiting.deadline":          {"; this task has {minutes} minutes left", "，這個 task 的時限還剩 {minutes} 分鐘", "。このタスクの残り時間は {minutes} 分です", "；此任务还剩 {minutes} 分钟", "; 이 작업의 남은 시간은 {minutes}분입니다", "; a esta tarea le quedan {minutes} minutos", "; restam {minutes} minutos para esta tarefa", " ; il reste {minutes} minutes à cette tâche", "; diese Aufgabe hat noch {minutes} Minuten"},
	"waiting.end":               {".", "。", "。", "。", ".", ".", ".", ".", "."},
	"decision.title":            {"Waiting for your decision: {question}", "等你決定：{question}", "判断をお待ちしています：{question}", "等你决定：{question}", "결정을 기다리는 중: {question}", "Esperando tu decisión: {question}", "Aguardando sua decisão: {question}", "En attente de votre décision : {question}", "Wartet auf Ihre Entscheidung: {question}"},
	"decision.body":             {"Options: {options}. If nobody answers, the default “{default}” takes effect at {at}.", "選項：{options}。沒有回答的話，{at} 起照「{default}」做。", "選択肢：{options}。回答がない場合は、{at} から既定の「{default}」が適用されます。", "选项：{options}。如果无人回答，将从 {at} 起按默认选项“{default}”执行。", "선택지: {options}. 응답이 없으면 {at}부터 기본 선택지 ‘{default}’가 적용됩니다.", "Opciones: {options}. Si nadie responde, la opción predeterminada «{default}» se aplicará a partir de {at}.", "Opções: {options}. Se ninguém responder, a opção padrão “{default}” passará a valer em {at}.", "Options : {options}. Sans réponse, l’option par défaut « {default} » s’appliquera à partir de {at}.", "Optionen: {options}. Wenn niemand antwortet, gilt ab {at} die Vorgabe „{default}“."},
	"decision.option_separator": {" / ", "／", "／", "／", " / ", " / ", " / ", " / ", " / "},
	"capacity.amount":           {"{used} / {limit} ({percent})", "{used}／{limit}（{percent}）", "{used}／{limit}（{percent}）", "{used}／{limit}（{percent}）", "{used} / {limit} ({percent})", "{used} / {limit} ({percent})", "{used} / {limit} ({percent})", "{used} / {limit} ({percent})", "{used} / {limit} ({percent})"},
	"capacity.unit.characters":  {" characters", " 字", " 文字", " 字", "자", " caracteres", " caracteres", " caractères", " Zeichen"},
	"capacity.unit.seconds":     {" seconds", " 秒", " 秒", " 秒", "초", " segundos", " segundos", " secondes", " Sekunden"},
	"capacity.unit.rows":        {" rows", " 筆", " 行", " 条", "개", " filas", " linhas", " lignes", " Zeilen"},
	"capacity.recovered.title":  {"Capacity recovered: {name}", "容量已恢復：{name}", "容量が回復しました：{name}", "容量已恢复：{name}", "용량 회복: {name}", "Capacidad recuperada: {name}", "Capacidade recuperada: {name}", "Capacité rétablie : {name}", "Kapazität wiederhergestellt: {name}"},
	"capacity.recovered.body":   {"{name} is back to {amount}, below the warning threshold. No action is needed.", "{name} 回到 {amount}，已經低於告警門檻，不用做什麼。", "{name} は {amount} に戻り、警告しきい値を下回りました。対応は不要です。", "{name} 已回落至 {amount}，低于警告阈值。无需处理。", "{name}의 사용량이 {amount}(으)로 돌아와 경고 기준 아래입니다. 조치가 필요하지 않습니다.", "{name} ha vuelto a {amount}, por debajo del umbral de alerta. No hace falta actuar.", "{name} voltou a {amount}, abaixo do limite de alerta. Nenhuma ação é necessária.", "{name} est revenu à {amount}, sous le seuil d’alerte. Aucune action nécessaire.", "{name} liegt wieder bei {amount} und damit unter der Warnschwelle. Keine Maßnahme erforderlich."},
	"capacity.critical.title":   {"Capacity nearly full: {name} ({percent})", "容量快滿了：{name}（{percent}）", "容量がまもなく満杯です：{name}（{percent}）", "容量即将满：{name}（{percent}）", "용량 부족: {name} ({percent})", "Capacidad casi llena: {name} ({percent})", "Capacidade quase cheia: {name} ({percent})", "Capacité presque pleine : {name} ({percent})", "Kapazität fast voll: {name} ({percent})"},
	"capacity.full.title":       {"Capacity full: {name}", "容量已滿：{name}", "容量が満杯です：{name}", "容量已满：{name}", "용량 가득 참: {name}", "Capacidad llena: {name}", "Capacidade cheia: {name}", "Capacité pleine : {name}", "Kapazität voll: {name}"},
	"capacity.used":             {"{name} is using {amount}. {consequence}.", "{name} 用了 {amount}。{consequence}。", "{name} の使用量は {amount} です。{consequence}。", "{name} 已使用 {amount}。{consequence}。", "{name} 사용량은 {amount}입니다. {consequence}.", "{name} usa {amount}. {consequence}.", "{name} usa {amount}. {consequence}.", "{name} utilise {amount}. {consequence}.", "{name} nutzt {amount}. {consequence}."},
	"capacity.projected":        {"At the recent rate, it will be full on {at}.", "照最近的速度，{at} 會滿。", "最近のペースでは {at} に満杯になります。", "按最近的速度，将于 {at} 满。", "최근 속도라면 {at}에 가득 찹니다.", "Al ritmo reciente, se llenará el {at}.", "No ritmo recente, ficará cheia em {at}.", "Au rythme actuel, elle sera pleine le {at}.", "Bei der aktuellen Rate ist sie am {at} voll."},
	"capacity.person":           {"You need to decide what to remove; see Capacity in Settings.", "要騰出空間得由你決定；細節在設定頁的「容量」。", "空き容量を作るにはあなたの判断が必要です。詳細は設定の「容量」をご覧ください。", "腾出空间需要你决定；详情见设置中的“容量”。", "공간을 확보하려면 직접 결정해야 합니다. 자세한 내용은 설정의 용량 항목을 확인하세요.", "Debes decidir qué quitar; consulta Capacidad en Configuración.", "Você precisa decidir o que remover; veja Capacidade em Configurações.", "Vous devez choisir quoi supprimer ; consultez Capacité dans les paramètres.", "Sie müssen entscheiden, was entfernt wird; siehe Kapazität in den Einstellungen."},
	"capacity.daemon":           {"The daemon will handle this by the rule above; see Capacity in Settings.", "daemon 會照上面的規則自己處理；細節在設定頁的「容量」。", "daemon が上記の規則に従って処理します。詳細は設定の「容量」をご覧ください。", "daemon 会按上述规则自行处理；详情见设置中的“容量”。", "daemon이 위 규칙에 따라 처리합니다. 자세한 내용은 설정의 용량 항목을 확인하세요.", "El daemon lo gestionará según la regla anterior; consulta Capacidad en Configuración.", "O daemon cuidará disso conforme a regra acima; veja Capacidade em Configurações.", "Le daemon suivra la règle ci-dessus ; consultez Capacité dans les paramètres.", "Der Daemon verarbeitet dies nach obiger Regel; siehe Kapazität in den Einstellungen."},
	"capacity.young.title":      {"Image cache too small: {name}", "圖片快取太小：{name}", "画像キャッシュが小さすぎます：{name}", "图片缓存过小：{name}", "이미지 캐시가 너무 작음: {name}", "Caché de imágenes demasiado pequeña: {name}", "Cache de imagens muito pequeno: {name}", "Cache d’images trop petit : {name}", "Bildcache zu klein: {name}"},
	"capacity.young.body":       {"{name}: {count} images sent in the past day were removed by the {drops} byte limit before they were a day old. Their paths are already in the conversation, so the assistant cannot read them later, for example after compact or resume. Send an image again if needed. See Capacity in Settings.", "{name}：過去一天有 {count} 張送出不到一天的圖，被 {drops} 的位元組上限提早刪掉了。這些路徑已經打進對話，assistant 之後回頭讀（例如 compact 或 resume 之後）會讀不到；需要時請重新貼一次。細節在設定頁的「容量」。", "{name}：過去1日に送られた画像 {count} 枚が、1日経つ前に {drops} のバイト上限で削除されました。パスは会話に残りますが、assistant は compact や resume の後などに読み直せません。必要なら画像を再送してください。詳細は設定の「容量」をご覧ください。", "{name}：过去一天发送的 {count} 张图片尚未满一天，就因 {drops} 的字节上限被删除。路径已写入对话，但 assistant 之后（例如 compact 或 resume 后）无法重新读取。需要时请重新发送图片。详情见设置中的“容量”。", "{name}: 지난 하루에 보낸 이미지 {count}개가 하루가 지나기 전에 {drops} 바이트 제한으로 삭제되었습니다. 경로는 대화에 남지만 assistant가 compact 또는 resume 후 다시 읽을 수 없습니다. 필요하면 이미지를 다시 보내세요. 자세한 내용은 설정의 용량 항목을 확인하세요.", "{name}: {count} imágenes enviadas durante el último día se borraron antes de cumplir un día por el límite de bytes de {drops}. Sus rutas siguen en la conversación, pero el asistente no podrá volver a leerlas, por ejemplo tras compact o resume. Envía la imagen otra vez si hace falta. Consulta Capacidad en Configuración.", "{name}: {count} imagens enviadas no último dia foram removidas antes de completar um dia pelo limite de bytes de {drops}. Os caminhos permanecem na conversa, mas o assistente não poderá relê-las, por exemplo após compact ou resume. Reenvie a imagem se necessário. Veja Capacidade em Configurações.", "{name} : {count} images envoyées durant la dernière journée ont été supprimées avant un jour par la limite d’octets de {drops}. Leurs chemins restent dans la conversation, mais l’assistant ne pourra plus les relire, par exemple après compact ou resume. Renvoyez l’image si nécessaire. Consultez Capacité dans les paramètres.", "{name}: {count} im letzten Tag gesendete Bilder wurden vor Ablauf eines Tages durch das Bytelimit von {drops} entfernt. Ihre Pfade stehen im Gespräch, aber der Assistent kann sie später, etwa nach compact oder resume, nicht mehr lesen. Senden Sie das Bild bei Bedarf erneut. Siehe Kapazität in den Einstellungen."},
	"capacity.refuse.evidence":  {"Once full, new writes are refused and /v1/health reports capacity_exhausted; the daemon removes nothing on its own", "滿了之後新的寫入會被拒絕，/v1/health 會回 capacity_exhausted，daemon 不會自己刪任何東西", "満杯になると新しい書き込みを拒否し、/v1/health は capacity_exhausted を返します。daemon は自動削除しません", "满后新写入将被拒绝，/v1/health 返回 capacity_exhausted；daemon 不会自行删除内容", "가득 차면 새 쓰기를 거부하고 /v1/health는 capacity_exhausted를 반환합니다. daemon은 임의로 삭제하지 않습니다", "Al llenarse, se rechazan nuevas escrituras y /v1/health devuelve capacity_exhausted; el daemon no elimina nada por su cuenta", "Ao encher, novas gravações são recusadas e /v1/health retorna capacity_exhausted; o daemon não remove nada sozinho", "Une fois plein, les nouvelles écritures sont refusées et /v1/health renvoie capacity_exhausted ; le daemon ne supprime rien seul", "Bei voller Kapazität werden neue Schreibvorgänge abgelehnt und /v1/health meldet capacity_exhausted; der Daemon löscht nichts selbst"},
	"capacity.refuse.person":    {"Once full, new items are refused; the daemon removes nothing on its own", "滿了之後新的會被拒絕，daemon 不會自己刪任何東西", "満杯になると新しい項目を拒否し、daemon は自動削除しません", "满后新内容将被拒绝；daemon 不会自行删除内容", "가득 차면 새 항목을 거부하며 daemon은 임의로 삭제하지 않습니다", "Al llenarse, se rechazan nuevos elementos; el daemon no elimina nada por su cuenta", "Ao encher, novos itens são recusados; o daemon não remove nada sozinho", "Une fois plein, les nouveaux éléments sont refusés ; le daemon ne supprime rien seul", "Bei voller Kapazität werden neue Einträge abgelehnt; der Daemon löscht nichts selbst"},
	"capacity.refuse.other":     {"Once full, new requests are refused and asked to retry later; existing items remain", "滿了之後新的請求會被拒絕、請對方稍後再試，已經在裡面的不會被丟掉", "満杯になると新しい要求は拒否され、後で再試行するよう求められます。既存の項目は残ります", "满后新请求会被拒绝并要求稍后重试；现有内容会保留", "가득 차면 새 요청을 거부하고 나중에 다시 시도하도록 안내합니다. 기존 항목은 유지됩니다", "Al llenarse, se rechazan nuevas solicitudes y se pide reintentar más tarde; los elementos existentes permanecen", "Ao encher, novas solicitações são recusadas e devem ser repetidas depois; os itens existentes permanecem", "Une fois plein, les nouvelles demandes sont refusées et leurs auteurs invités à réessayer plus tard ; les éléments existants restent", "Bei voller Kapazität werden neue Anfragen abgelehnt und sollen später wiederholt werden; bestehende Einträge bleiben"},
	"capacity.evict":            {"Once full, the oldest items are removed", "滿了之後會淘汰最舊的", "満杯になると古い項目から削除します", "满后会淘汰最旧内容", "가득 차면 가장 오래된 항목을 제거합니다", "Al llenarse, se eliminan los elementos más antiguos", "Ao encher, os itens mais antigos são removidos", "Une fois plein, les éléments les plus anciens sont supprimés", "Bei voller Kapazität werden die ältesten Einträge entfernt"},
	"capacity.expire":           {"Once full, items past the retry window expire", "滿了之後過了重試視窗的會過期", "満杯になると再試行期間を過ぎた項目は期限切れになります", "满后超过重试窗口的内容会过期", "가득 차면 재시도 기간이 지난 항목은 만료됩니다", "Al llenarse, caducan los elementos fuera de la ventana de reintento", "Ao encher, os itens fora da janela de nova tentativa expiram", "Une fois plein, les éléments hors de la période de nouvel essai expirent", "Bei voller Kapazität verfallen Einträge nach dem Wiederholungsfenster"},
	"capacity.rotate.audit":     {"Once full, a new segment starts and old segments remain", "滿了之後會輪替成新的分段，舊的分段不刪", "満杯になると新しいセグメントに切り替わり、古いセグメントは残ります", "满后会轮换到新分段，旧分段保留", "가득 차면 새 구간으로 전환하고 이전 구간은 유지합니다", "Al llenarse, comienza un segmento nuevo y se conservan los anteriores", "Ao encher, começa um novo segmento e os anteriores permanecem", "Une fois plein, un nouveau segment commence et les anciens sont conservés", "Bei voller Kapazität beginnt ein neues Segment; alte Segmente bleiben erhalten"},
	"capacity.rotate":           {"Once full, a new segment starts and the oldest is removed", "滿了之後會輪替成新的分段，最舊的分段會被刪掉", "満杯になると新しいセグメントに切り替わり、最も古いものは削除されます", "满后会轮换到新分段，最旧分段会被删除", "가득 차면 새 구간으로 전환하고 가장 오래된 구간을 삭제합니다", "Al llenarse, comienza un segmento nuevo y se elimina el más antiguo", "Ao encher, começa um novo segmento e o mais antigo é removido", "Une fois plein, un nouveau segment commence et le plus ancien est supprimé", "Bei voller Kapazität beginnt ein neues Segment und das älteste wird entfernt"},
	"capacity.summarize":        {"Once full, content is summarized before the original is removed", "滿了之後會先摘要再移走原文", "満杯になると元の内容を削除する前に要約します", "满后会先摘要再移除原文", "가득 차면 원문을 제거하기 전에 요약합니다", "Al llenarse, el contenido se resume antes de quitar el original", "Ao encher, o conteúdo é resumido antes da remoção do original", "Une fois plein, le contenu est résumé avant la suppression de l’original", "Bei voller Kapazität wird der Inhalt zusammengefasst, bevor das Original entfernt wird"},
	"capacity.coalesce":         {"Once full, only the newest value is kept", "滿了之後只留最新的值", "満杯になると最新の値だけを残します", "满后只保留最新值", "가득 차면 최신 값만 유지합니다", "Al llenarse, solo se conserva el valor más reciente", "Ao encher, apenas o valor mais recente é mantido", "Une fois plein, seule la valeur la plus récente est conservée", "Bei voller Kapazität bleibt nur der neueste Wert erhalten"},
	"capacity.disconnect":       {"Once full, readers that fall behind disconnect and reload", "滿了之後跟不上的讀者會被斷線、重新讀取", "満杯になると追いつけない読み手は切断され、再読み込みします", "满后跟不上的读取端会断开并重新读取", "가득 차면 뒤처진 읽기 연결을 끊고 다시 불러옵니다", "Al llenarse, los lectores rezagados se desconectan y vuelven a cargar", "Ao encher, leitores atrasados são desconectados e recarregam", "Une fois plein, les lecteurs en retard sont déconnectés puis rechargent", "Bei voller Kapazität werden zurückliegende Leser getrennt und laden neu"},
	"capacity.none.evidence":    {"Currently, full capacity does not refuse writes; only /v1/health reports capacity_exhausted", "目前滿了也不會拒絕寫入，只有 /v1/health 會回 capacity_exhausted", "現在は満杯でも書き込みを拒否せず、/v1/health のみ capacity_exhausted を返します", "目前即使满了也不会拒绝写入；只有 /v1/health 返回 capacity_exhausted", "현재 가득 차도 쓰기를 거부하지 않으며 /v1/health만 capacity_exhausted를 반환합니다", "Actualmente, la capacidad llena no rechaza escrituras; solo /v1/health devuelve capacity_exhausted", "Atualmente, a capacidade cheia não recusa gravações; apenas /v1/health retorna capacity_exhausted", "Actuellement, la saturation ne refuse pas les écritures ; seul /v1/health renvoie capacity_exhausted", "Derzeit werden Schreibvorgänge bei voller Kapazität nicht abgelehnt; nur /v1/health meldet capacity_exhausted"},
	"capacity.none":             {"Currently, full capacity does not refuse or remove anything", "目前滿了也不會拒絕或淘汰任何東西", "現在は満杯でも拒否や削除をしません", "目前即使满了也不会拒绝或淘汰任何内容", "현재 가득 차도 어떤 항목도 거부하거나 제거하지 않습니다", "Actualmente, la capacidad llena no rechaza ni elimina nada", "Atualmente, a capacidade cheia não recusa nem remove nada", "Actuellement, la saturation ne refuse ni ne supprime rien", "Derzeit wird bei voller Kapazität nichts abgelehnt oder entfernt"},
	"dead.title":                {"Completion notice not delivered", "完成通知沒有送達", "完了通知が届きませんでした", "完成通知未送达", "완료 알림이 전달되지 않았습니다", "Aviso de finalización no entregado", "Aviso de conclusão não entregue", "Notification de fin non remise", "Abschlussbenachrichtigung nicht zugestellt"},
	"dead.body":                 {"“{title}” has finished ({state}), but its notice was not acknowledged after {attempts} attempts. Open that session to read result.json; after resolving the cause, retry with POST /v1/orchestrator/completions/reconcile.", "「{title}」已經結束（{state}），但通知嘗試送出 {attempts} 次後仍未獲確認。打開那個 session 讀 result.json；修好原因後可以用 POST /v1/orchestrator/completions/reconcile 重送。", "「{title}」は終了しました（{state}）が、通知は {attempts} 回試しても確認されませんでした。そのセッションで result.json を読み、原因を解消してから POST /v1/orchestrator/completions/reconcile で再送してください。", "“{title}”已结束（{state}），但通知尝试发送 {attempts} 次后仍未获确认。打开该会话读取 result.json；解决原因后使用 POST /v1/orchestrator/completions/reconcile 重试。", "“{title}” 작업이 종료되었습니다({state}). 알림을 {attempts}번 시도했지만 확인되지 않았습니다. 해당 세션에서 result.json을 읽고 원인을 해결한 뒤 POST /v1/orchestrator/completions/reconcile로 다시 보내세요.", "“{title}” ha terminado ({state}), pero el aviso no se confirmó tras {attempts} intentos. Abre esa sesión para leer result.json; resuelve la causa y reintenta con POST /v1/orchestrator/completions/reconcile.", "“{title}” terminou ({state}), mas o aviso não foi confirmado após {attempts} tentativas. Abra a sessão para ler result.json; resolva a causa e reenvie com POST /v1/orchestrator/completions/reconcile.", "« {title} » est terminé ({state}), mais l’avis n’a pas été confirmé après {attempts} tentatives. Ouvrez cette session pour lire result.json ; corrigez la cause puis réessayez avec POST /v1/orchestrator/completions/reconcile.", "„{title}“ ist beendet ({state}), aber die Benachrichtigung wurde nach {attempts} Versuchen nicht bestätigt. Öffnen Sie die Sitzung und lesen Sie result.json; beheben Sie die Ursache und versuchen Sie es mit POST /v1/orchestrator/completions/reconcile erneut."},
	"dead.held":                 {"“{title}” has finished ({state}), but its root terminal could not receive text ({reason}), so the notice was not delivered. Read result.json; free that terminal, then retry with POST /v1/orchestrator/completions/reconcile.", "「{title}」已經結束（{state}），但它的 root 終端機一直不能打字（{reason}），通知沒有送進去。讀 result.json；把那個終端機空出來之後，用 POST /v1/orchestrator/completions/reconcile 重送。", "「{title}」は終了しました（{state}）が、root 端末に入力できず（{reason}）、通知を届けられませんでした。result.json を読み、端末を空けてから POST /v1/orchestrator/completions/reconcile で再送してください。", "“{title}”已结束（{state}），但 root 终端无法输入（{reason}），通知未送达。读取 result.json；腾出终端后使用 POST /v1/orchestrator/completions/reconcile 重试。", "“{title}” 작업이 종료되었습니다({state}). root 터미널에 입력할 수 없어({reason}) 알림을 전달하지 못했습니다. result.json을 읽고 터미널을 비운 뒤 POST /v1/orchestrator/completions/reconcile로 다시 보내세요.", "“{title}” ha terminado ({state}), pero no se pudo escribir en su terminal raíz ({reason}), así que el aviso no se entregó. Lee result.json; libera la terminal y reintenta con POST /v1/orchestrator/completions/reconcile.", "“{title}” terminou ({state}), mas não foi possível escrever no terminal raiz ({reason}), então o aviso não foi entregue. Leia result.json; libere o terminal e reenvie com POST /v1/orchestrator/completions/reconcile.", "« {title} » est terminé ({state}), mais son terminal racine ne pouvait pas recevoir de texte ({reason}) ; l’avis n’a pas été remis. Lisez result.json ; libérez le terminal, puis réessayez avec POST /v1/orchestrator/completions/reconcile.", "„{title}“ ist beendet ({state}), aber das Root-Terminal konnte keinen Text empfangen ({reason}); die Benachrichtigung wurde nicht zugestellt. Lesen Sie result.json; geben Sie das Terminal frei und versuchen Sie es mit POST /v1/orchestrator/completions/reconcile erneut."},
	"dead.reason.composer":      {"unsent text is in the composer", "輸入框裡有還沒送出的內容", "入力欄に未送信の内容があります", "输入框中有未发送的内容", "입력창에 보내지 않은 내용이 있습니다", "hay texto sin enviar en el editor", "há texto não enviado no campo de entrada", "du texte non envoyé est dans la zone de saisie", "im Eingabefeld steht ungesendeter Text"},
	"dead.reason.choosing":      {"a choice menu is waiting for an answer", "畫面停在一個等人回答的選單", "回答待ちの選択メニューが表示されています", "屏幕停留在等待回答的选项菜单", "선택 메뉴가 답변을 기다리고 있습니다", "hay un menú de opciones esperando respuesta", "um menu de opções aguarda resposta", "un menu de choix attend une réponse", "ein Auswahlmenü wartet auf eine Antwort"},
	"dead.reason.lane":          {"another operation has held the terminal", "那個終端機一直被別人佔用", "別の操作が端末を占有しています", "终端一直被其他操作占用", "다른 작업이 터미널을 점유하고 있습니다", "otra operación mantiene ocupada la terminal", "outra operação mantém o terminal ocupado", "une autre opération occupe le terminal", "ein anderer Vorgang belegt das Terminal"},
	"dead.reason.queued":        {"an earlier notice is still unread in the composer", "先前那一份還排在輸入框裡沒被讀到", "先の通知が入力欄でまだ読まれていません", "先前的通知仍在输入框中未被读取", "이전 알림이 입력창에서 아직 읽히지 않았습니다", "un aviso anterior sigue sin leerse en el editor", "um aviso anterior ainda não foi lido no campo de entrada", "un avis précédent n’a pas encore été lu dans la zone de saisie", "eine frühere Nachricht im Eingabefeld wurde noch nicht gelesen"},
	"board.title":               {"Board item completed", "看板項目已完成", "ボード項目が完了しました", "看板项目已完成", "보드 항목 완료", "Elemento del tablero completado", "Item do quadro concluído", "Élément du tableau terminé", "Board-Eintrag abgeschlossen"},
	"board.body":                {"{title} is complete.", "「{title}」已完成", "「{title}」が完了しました", "“{title}”已完成", "“{title}” 항목이 완료되었습니다", "“{title}” está completado.", "“{title}” foi concluído.", "« {title} » est terminé.", "„{title}“ ist abgeschlossen."},
	"note.title":                {"New note", "有新便條紙", "新しいメモ", "有新便条", "새 메모", "Nueva nota", "Nova nota", "Nouvelle note", "Neue Notiz"},
	"note.body":                 {"Open “{title}” in the Session.", "到 Session 查看「{title}」。", "セッションで「{title}」を開いてください。", "在会话中查看“{title}”。", "세션에서 ‘{title}’ 메모를 확인하세요.", "Abre «{title}» en la sesión.", "Abra “{title}” na sessão.", "Ouvrez « {title} » dans la session.", "Öffnen Sie „{title}“ in der Sitzung."},
	"push.test":                 {"This is a test notification. Notifications are connected.", "這是一則測試通知，該接的都接好了。", "これはテスト通知です。通知の接続は完了しています。", "这是一条测试通知。通知连接已就绪。", "테스트 알림입니다. 알림 연결이 완료되었습니다.", "Esta es una notificación de prueba. Las notificaciones están conectadas.", "Esta é uma notificação de teste. As notificações estão conectadas.", "Ceci est une notification de test. Les notifications sont connectées.", "Dies ist eine Testbenachrichtigung. Benachrichtigungen sind verbunden."},
}

var placeholder = regexp.MustCompile(`\{[a-z]+\}`)

// Resolve chooses a shipped language. Unsupported, malformed and ambiguous
// tags use English. Script subtags take precedence over region inference.
func Resolve(tag string) string {
	if len(tag) < 2 || len(tag) > 35 || strings.TrimSpace(tag) != tag {
		return "en"
	}
	parts := strings.Split(strings.ToLower(strings.ReplaceAll(tag, "_", "-")), "-")
	for i, p := range parts {
		if p == "" || len(p) > 8 || i == 0 && len(p) < 2 {
			return "en"
		}
		for _, c := range p {
			if c < 'a' || c > 'z' {
				if i == 0 || c < '0' || c > '9' {
					return "en"
				}
			}
		}
	}
	switch parts[0] {
	case "en", "ja", "ko", "es", "fr", "de":
		return parts[0]
	case "pt":
		return "pt-BR"
	case "zh":
		script := ""
		for _, p := range parts[1:] {
			if len(p) != 4 || !productCopyLetters(p) {
				continue
			}
			if p != "hant" && p != "hans" || script != "" && script != p {
				return "en"
			}
			script = p
		}
		if script == "hant" {
			return "zh-Hant"
		}
		if script == "hans" {
			return "zh-Hans"
		}
		for _, p := range parts[1:] {
			if p == "tw" || p == "hk" || p == "mo" {
				return "zh-Hant"
			}
			if p == "cn" || p == "sg" {
				return "zh-Hans"
			}
		}
	}
	return "en"
}

func productCopyLetters(part string) bool {
	for _, c := range part {
		if c < 'a' || c > 'z' {
			return false
		}
	}
	return true
}

// Coverage counts translated keys and missing keys for a shipped language.
// English itself is the source, so its missing count is always zero.
func Coverage(language string) (translated, missing int) {
	index := languageIndex(Resolve(language))
	for _, forms := range notices {
		if index == 0 || valid(forms[0], forms[index]) {
			translated++
		} else {
			missing++
		}
	}
	return
}

// Validate rejects an entire catalog when a present entry has broken
// placeholders or markup. Only missing keys in the seven secondary languages
// are allowed; those gaps are counted by Coverage and use English per key.
func Validate(language string) error {
	selected := Resolve(language)
	index := languageIndex(selected)
	for key, forms := range notices {
		if !valid(forms[0], forms[0]) {
			return fmt.Errorf("English notification key %s is invalid", key)
		}
		if index == 0 {
			continue
		}
		if forms[index] == "" && index > 1 {
			continue
		}
		if !valid(forms[0], forms[index]) {
			return fmt.Errorf("notification key %s is invalid in %s", key, selected)
		}
	}
	return nil
}

// Format renders one fixed sentence. A missing secondary key uses its English
// sentence; a malformed present key sends the whole catalog back to English.
func Format(language, key string, values map[string]string) string {
	if err := Validate("en"); err != nil {
		log.Printf("productcopy: %v; no safe notification text is available", err)
		return ""
	}
	selected := Resolve(language)
	if selected != "en" {
		if err := Validate(selected); err != nil {
			log.Printf("productcopy: %v; using the English catalog", err)
			selected = "en"
		}
	}
	if selected != "en" && selected != "zh-Hant" {
		translated, missing := Coverage(selected)
		if missing > 0 {
			log.Printf("productcopy: notification catalog %s covers %d keys; %d use English fallback", selected, translated, missing)
		}
	}
	forms, ok := notices[key]
	if !ok {
		return ""
	}
	template := forms[languageIndex(selected)]
	if template == "" {
		template = forms[0]
	}
	return placeholder.ReplaceAllStringFunc(template, func(match string) string {
		return values[strings.Trim(match, "{}")]
	})
}

func languageIndex(language string) int {
	for i, candidate := range Languages {
		if candidate == language {
			return i
		}
	}
	return 0
}

func valid(source, translated string) bool {
	if translated == "" || strings.ContainsAny(translated, "<>{}") && !onlyPlaceholders(translated) {
		return false
	}
	want, got := placeholderNames(source), placeholderNames(translated)
	if len(want) != len(got) {
		return false
	}
	for k := range want {
		if !got[k] {
			return false
		}
	}
	return true
}

func onlyPlaceholders(s string) bool {
	return !strings.ContainsAny(placeholder.ReplaceAllString(s, ""), "<>{}")
}

func placeholderNames(s string) map[string]bool {
	out := map[string]bool{}
	for _, p := range placeholder.FindAllString(s, -1) {
		out[p] = true
	}
	return out
}
