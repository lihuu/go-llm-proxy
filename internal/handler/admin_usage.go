package handler

import (
	"log/slog"
	"net/http"
	"strconv"

	"go-llm-proxy/internal/httputil"
)

// UsagePage renders the usage dashboard inside the Admin shell.
func (h *AdminHandler) UsagePage(w http.ResponseWriter, r *http.Request) {
	bodyHTML := `
<div class="summary-cards" id="summaryCards"></div>
<div class="card">
<div class="card-header">
<h2 id="chartTitle">Daily Tokens</h2>
<div style="display:flex;gap:8px;align-items:center">
<div class="toggle-group">
<button class="toggle-btn" id="toggleRequests" onclick="setChartMode('requests')">Requests</button>
<button class="toggle-btn active" id="toggleTokens" onclick="setChartMode('tokens')">Tokens</button>
</div>
<select id="periodSelect" onchange="loadData()">
<option value="7">Last 7 days</option>
<option value="30" selected>Last 30 days</option>
<option value="90">Last 90 days</option>
</select>
</div>
</div>
<div id="dailyChart"></div>
</div>
<div class="card">
<h2>Users</h2>
<div class="table-wrap"><table class="data-table">
<thead><tr><th>Name</th><th>Key</th><th>Requests</th><th>Tokens</th><th>Input</th><th>Output</th><th>Cache R</th><th>Cache W</th><th>Active Days</th><th>Last Seen</th></tr></thead>
<tbody id="usersBody"></tbody>
</table></div>
</div>
<div class="card">
<h2>Models</h2>
<div class="table-wrap"><table class="data-table">
<thead><tr><th>Model</th><th>Requests</th><th>Users</th><th>Tokens</th><th>Input</th><th>Output</th><th>Cache R</th><th>Cache W</th><th>Avg Latency</th><th>Avg TTFB</th></tr></thead>
<tbody id="modelsBody"></tbody>
</table></div>
</div>`

	scriptJS := `
var MODEL_COLORS=["#1a56db","#047857","#b45309","#7c3aed","#db2777","#0d9488","#ca8a04","#dc2626","#4f46e5","#059669","#d97706","#9333ea"];
var chartMode="tokens";
var lastData=null;
(function(){loadData()})();
function setChartMode(mode){
	chartMode=mode;
	document.getElementById("toggleRequests").classList.toggle("active",mode==="requests");
	document.getElementById("toggleTokens").classList.toggle("active",mode==="tokens");
	document.getElementById("chartTitle").textContent=mode==="tokens"?"Daily Tokens":"Daily Requests";
	if(lastData)renderChart(lastData.daily,lastData.daily_models);
}
function loadData(){
	var days=document.getElementById("periodSelect").value;
	fetch("/admin/usage/data?days="+days)
		.then(function(r){return r.json()})
		.then(function(d){renderData(d)})
		.catch(function(e){console.error(e)});
}
function renderData(d){
	lastData=d;
	var t=d.totals;
	var sc=document.getElementById("summaryCards");
	sc.innerHTML=
		summaryCard("Total Requests",fmtNum(t.requests))+
		summaryCard("Input Tokens",fmtNum(t.input_tokens))+
		summaryCard("Output Tokens",fmtNum(t.output_tokens))+
		summaryCard("Cache Read",fmtNum(t.cache_read_tokens))+
		summaryCard("Cache Write",fmtNum(t.cache_write_tokens))+
		summaryCard("Active Users",t.users)+
		summaryCard("Error Rate",t.error_rate.toFixed(1)+"%");
	renderChart(d.daily,d.daily_models);
	renderTable("usersBody",d.users,function(u){
		return "<td>"+esc(u.name)+"</td><td><code>"+esc(u.key_hash)+"</code></td>"+
			"<td>"+fmtNum(u.requests)+"</td><td>"+fmtNum(u.total_tokens)+"</td>"+
			"<td>"+fmtNum(u.input_tokens)+"</td><td>"+fmtNum(u.output_tokens)+"</td>"+
			"<td>"+fmtNum(u.cache_read_tokens)+"</td><td>"+fmtNum(u.cache_write_tokens)+"</td>"+
			"<td>"+u.active_days+"</td><td>"+esc(u.last_seen)+"</td>";
	});
	renderTable("modelsBody",d.models,function(m){
		return "<td>"+esc(m.model)+"</td><td>"+fmtNum(m.requests)+"</td>"+
			"<td>"+m.users+"</td><td>"+fmtNum(m.total_tokens)+"</td>"+
			"<td>"+fmtNum(m.input_tokens)+"</td><td>"+fmtNum(m.output_tokens)+"</td>"+
			"<td>"+fmtNum(m.cache_read_tokens)+"</td><td>"+fmtNum(m.cache_write_tokens)+"</td>"+
			"<td>"+Math.round(m.avg_latency_ms)+" ms</td>"+
			"<td>"+Math.round(m.avg_ttfb_ms)+" ms</td>";
	});
}
function summaryCard(label,value){
	return "<div class=\"summary-card\"><div class=\"summary-value\">"+value+"</div><div class=\"summary-label\">"+label+"</div></div>";
}
function renderChart(rows,modelRows){
	var el=document.getElementById("dailyChart");
	if(!rows||rows.length===0){el.innerHTML="<p style=\"color:var(--muted);padding:20px 0\">No data for this period.</p>";return;}
	var useTokens=chartMode==="tokens";
	var valKey=useTokens?"total_tokens":"requests";
	var valLabel=useTokens?"tokens":"requests";
	var models=[];
	var modelSet={};
	if(modelRows){for(var i=0;i<modelRows.length;i++){if(!modelSet[modelRows[i].model]){modelSet[modelRows[i].model]=1;models.push(modelRows[i].model);}}}
	var dateMap={};
	for(var i=0;i<rows.length;i++){dateMap[rows[i].date]={total:rows[i][valKey],models:{}};}
	if(modelRows){for(var i=0;i<modelRows.length;i++){var dm=modelRows[i];if(dateMap[dm.date])dateMap[dm.date].models[dm.model]=dm[valKey];}}
	var max=0;
	for(var i=0;i<rows.length;i++){if(rows[i][valKey]>max)max=rows[i][valKey];}
	var html="";
	if(models.length>1){
		html+="<div class=\"chart-legend\">";
		for(var i=0;i<models.length;i++){
			var c=MODEL_COLORS[i%MODEL_COLORS.length];
			html+="<span class=\"legend-item\"><span class=\"legend-swatch\" style=\"background:"+c+"\"></span>"+esc(models[i])+"</span>";
		}
		html+="</div>";
	}
	html+="<div class=\"bars\">";
	for(var i=0;i<rows.length;i++){
		var r=rows[i];
		var val=r[valKey];
		var pct=max>0?(val/max*100):0;
		var dateLabel=r.date.substring(5);
		var dm=dateMap[r.date];
		var inner="";
		if(models.length>1&&dm){
			var segments=Object.keys(dm.models).sort(function(a,b){return dm.models[b]-dm.models[a];});
			for(var j=0;j<segments.length;j++){
				var segPct=max>0?(dm.models[segments[j]]/max*100):0;
				var ci=models.indexOf(segments[j]);
				var c=MODEL_COLORS[(ci<0?j:ci)%MODEL_COLORS.length];
				inner+="<div class=\"bar-segment\" style=\"height:"+segPct+"%;background:"+c+"\" title=\""+esc(segments[j])+": "+fmtNum(dm.models[segments[j]])+" "+valLabel+"\"></div>";
			}
		}else{
			inner="<div class=\"bar-segment\" style=\"height:"+pct+"%;background:var(--blue)\"></div>";
		}
		html+="<div class=\"bar-group\" title=\""+esc(r.date)+": "+fmtNum(r.requests)+" requests, "+fmtNum(r.total_tokens)+" tokens\">"+
			"<div class=\"bar-stack\">"+inner+"</div>"+
			"<div class=\"bar-label\">"+esc(dateLabel)+"</div></div>";
	}
	html+="</div>";
	el.innerHTML=html;
}
function renderTable(id,rows,cellFn){
	var tbody=document.getElementById(id);
	var html="";
	for(var i=0;i<rows.length;i++){html+="<tr>"+cellFn(rows[i])+"</tr>";}
	if(!rows.length)html="<tr><td colspan=\"99\" style=\"text-align:center;color:var(--muted);padding:16px\">No data</td></tr>";
	tbody.innerHTML=html;
}
function fmtNum(n){
	if(typeof n!=="number")return String(n);
	return n.toLocaleString();
}
function esc(s){var d=document.createElement("div");d.textContent=s;return d.innerHTML;}
`

	h.renderShell(w, "usage", "Usage — Admin", bodyHTML, scriptJS)
}

// UsageData serves the usage dashboard JSON data, authenticated via Admin session.
func (h *AdminHandler) UsageData(w http.ResponseWriter, r *http.Request) {
	if h.ul == nil {
		httputil.WriteError(w, http.StatusNotFound, "usage logging not enabled")
		return
	}
	days := 30
	if d := r.URL.Query().Get("days"); d != "" {
		if v, err := strconv.Atoi(d); err == nil && v > 0 && v <= 365 {
			days = v
		}
	}
	data, err := h.ul.QueryDashboardData(days)
	if err != nil {
		slog.Error("admin usage query failed", "error", err)
		httputil.WriteError(w, http.StatusInternalServerError, "query failed")
		return
	}
	writeJSON(w, http.StatusOK, data)
}
