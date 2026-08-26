(()=>{
  const $=(s,r=document)=>r.querySelector(s),$$=(s,r=document)=>[...r.querySelectorAll(s)];
  const menu=$('[data-menu-toggle]'),sidebar=$('.sidebar'),backdrop=$('[data-menu-backdrop]');
  const setMenu=open=>{sidebar?.classList.toggle('open',open);document.body.classList.toggle('menu-open',open);menu?.setAttribute('aria-expanded',String(open))};
  if(menu)menu.addEventListener('click',()=>setMenu(!sidebar?.classList.contains('open')));
  backdrop?.addEventListener('click',()=>setMenu(false));
  $$('.nav a',sidebar||document).forEach(link=>link.addEventListener('click',()=>setMenu(false)));
  document.addEventListener('keydown',e=>{if(e.key==='Escape')setMenu(false)});
  const notice=$('[data-auto-dismiss]');if(notice)setTimeout(()=>notice.remove(),4200);
  $$('form[data-confirm]').forEach(form=>form.addEventListener('submit',e=>{if(!confirm(form.dataset.confirm))e.preventDefault()}));
  $('[data-print]')?.addEventListener('click',()=>window.print());

  const workForm=$('[data-worklog-form]');
  if(workForm){
    const rows=$('[data-work-rows]'),template=$('#work-row-template'),payload=$('[data-work-payload]',workForm),catalog=new Map();
    $$('[data-project-subitem-source]').forEach(source=>{
      const projectID=source.dataset.projectId||'';
      if(!catalog.has(projectID))catalog.set(projectID,[]);
      catalog.get(projectID).push({id:source.dataset.subitemId||'',name:source.dataset.name||'',area:source.dataset.area||'0',structure:source.dataset.structure||''});
    });
    const ensureWorkCategory=row=>{
      let field=$('[data-work-category-field]',row);
      if(!field){
        field=document.createElement('label');field.className='category-field';field.dataset.workCategoryField='';
        field.innerHTML='工作类别<select name="work_category[]" data-work-category><option value="regular">常规项目工作</option><option value="site">工地驻场</option></select>';
        $('.project-field',row)?.insertAdjacentElement('afterend',field);
      }
      const select=$('[data-work-category]',field);
      if(select&&!select.dataset.ready){select.value=row.dataset.workCategory==='site'?'site':'regular';select.dataset.ready='1'}
      let note=$('[data-site-resident-note]',row);
      if(!note){
        note=document.createElement('p');note.className='site-resident-note';note.dataset.siteResidentNote='';
        note.innerHTML='<strong>工地驻场</strong><span>仅指轮换或长期驻场；偶尔去半天验收不算驻场。</span>';
        $('.work-details-grid',row)?.insertAdjacentElement('beforebegin',note);
      }
      return select;
    };
    const applySubitem=row=>{
      const select=$('[data-project-subitem-select]',row),choice=select?.selectedOptions?.[0],hidden=$('[data-project-subitem-value]',row),manual=$('input[data-work-subitem]',row);
      if(hidden)hidden.value=choice?.value||'';
      if(choice?.value){
        if(manual)manual.value=choice.dataset.name||choice.textContent||'';
        const area=$('input[data-work-area]',row),structure=$('input[data-work-structure]',row);
        if(area)area.value=choice.dataset.area||'0';
        if(structure)structure.value=choice.dataset.structure||'';
      }
    };
    const populateSubitems=(row,projectID,preferredID)=>{
      const select=$('[data-project-subitem-select]',row),manual=$('input[data-work-subitem]',row);
      if(!select)return;
      const items=catalog.get(projectID)||[],current=preferredID||select.dataset.selectedSubitemId||'';
      select.replaceChildren();
      const first=document.createElement('option');first.value='';first.textContent=items.length?'请选择子项':'项目暂未维护子项';select.append(first);
      items.forEach(item=>{const option=document.createElement('option');option.value=item.id;option.textContent=item.name+(item.structure?' · '+item.structure:'');option.dataset.name=item.name;option.dataset.area=item.area;option.dataset.structure=item.structure;if(item.id===current)option.selected=true;select.append(option)});
      select.hidden=items.length===0;
      if(manual){manual.hidden=items.length>0;manual.readOnly=items.length>0}
      select.required=items.length>0;
      select.dataset.loadedProject=projectID;
      if(select.value)applySubitem(row);
    };
    workForm.addEventListener('submit',e=>{
      if(e.submitter?.matches('[data-clear-worklog]')){if(!confirm('确定清空本人本周全部实际工时和请假记录吗？此操作会写入审计日志。'))e.preventDefault();return}
      if(payload)payload.value=JSON.stringify($$('[data-work-row]',rows).map(row=>({
        entry_type:$('[data-entry-type]',row)?.value||'project',
        work_category:$('[data-work-category]',row)?.value||'regular',
        project_id:$('[data-project-select]',row)?.selectedOptions?.[0]?.dataset?.projectId||$('[data-project-select]',row)?.value||'',
        project_subitem_id:$('[data-project-subitem-value]',row)?.value||'',
        project_code:$('[data-project-select]',row)?.selectedOptions?.[0]?.dataset?.projectCode||'',
        hours:$('input[name="hours[]"]',row)?.value||'',
        work_subitem:$('input[data-work-subitem]',row)?.value||'',work_area:$('input[data-work-area]',row)?.value||'',work_structure:$('input[data-work-structure]',row)?.value||'',work_role:$('input[data-work-role]',row)?.value||'',
        other_description:$('.other-field input',row)?.value||'',end_participation:$('[data-end-check]',row)?.checked||false
      })).filter(entry=>(parseFloat(entry.hours)||0)>0));
    });
    const syncRow=row=>{
      const categorySelect=ensureWorkCategory(row),category=categorySelect?.value||'regular';
      row.classList.toggle('is-site',category==='site');
      const type=$('[data-entry-type]',row)?.value||'project';row.classList.toggle('is-other',type==='other');
      const project=$('[data-project-select]',row),selected=project?.selectedOptions?.[0],selectedID=selected?.dataset?.projectId||project?.value||'',projectValue=$('[data-project-value]',row);
      if(projectValue)projectValue.value=selectedID;
      const projectChanged=selectedID!==(row.dataset.projectId||'');
      if(type==='project'&&selectedID&&projectChanged){
        const definitions=[['workSubitem','input[data-work-subitem]'],['workArea','input[data-work-area]'],['workStructure','input[data-work-structure]'],['workRole','input[data-work-role]']];
        definitions.forEach(([key,selector])=>{const input=$(selector,row);if(input)input.value=selected?.dataset?.[key]||''});
        const subitemValue=$('[data-project-subitem-value]',row);if(subitemValue)subitemValue.value=selected?.dataset?.projectSubitemId||'';
        row.dataset.projectId=selectedID;
      }
      const subitemSelect=$('[data-project-subitem-select]',row);
      if(subitemSelect&&(projectChanged||subitemSelect.dataset.loadedProject!==selectedID)){
        populateSubitems(row,selectedID,projectChanged?(selected?.dataset?.projectSubitemId||''):($('[data-project-subitem-value]',row)?.value||''));
      }
      if(project)project.required=type==='project';
      const needsProjectDetails=type==='project'&&category!=='site';
      if(subitemSelect)subitemSelect.required=needsProjectDetails&&!subitemSelect.hidden;
      const role=$('input[data-work-role]',row);if(role)role.required=needsProjectDetails;
      const manual=$('input[data-work-subitem]',row);if(manual)manual.required=needsProjectDetails&&subitemSelect?.hidden;
      const other=$('.other-field input',row);if(other)other.required=type==='other';
      const check=$('[data-end-check]',row),hidden=$('[data-end-value]',row);if(check&&hidden)hidden.value=check.checked?'1':'0';
    };
    const refresh=()=>{
      $$('[data-work-row]',rows).forEach((row,i)=>{const index=$('.row-index',row);if(index)index.textContent=String(i+1).padStart(2,'0');syncRow(row)});
      const total=$$('input[name="hours[]"]',rows).reduce((sum,input)=>sum+(parseFloat(input.value)||0),0);const out=$('[data-total-hours]');if(out)out.textContent=total.toFixed(1);
    };
    rows.addEventListener('change',e=>{const row=e.target.closest('[data-work-row]');if(row){if(e.target.matches('[data-project-subitem-select]'))applySubitem(row);syncRow(row)}refresh()});
    rows.addEventListener('input',refresh);
    rows.addEventListener('click',e=>{const button=e.target.closest('[data-remove-work]');if(!button)return;const all=$$('[data-work-row]',rows);if(all.length===1){all[0].querySelectorAll('input').forEach(i=>{if(i.type!=='hidden')i.value=''});all[0].querySelectorAll('select').forEach(s=>s.selectedIndex=0);all[0].dataset.projectId='';return refresh()}button.closest('[data-work-row]').remove();refresh()});
    $('[data-add-work]')?.addEventListener('click',()=>{rows.append(template.content.cloneNode(true));refresh();$$('[data-work-row]',rows).at(-1)?.querySelector('select')?.focus()});
    const leave=$('[data-leave-input]'),leaveOut=$('[data-leave-output]');leave?.addEventListener('input',()=>{if(leaveOut)leaveOut.textContent=(parseFloat(leave.value)||0).toFixed(1)});refresh();
  }

  const subitemEditor=$('[data-project-subitems-editor]');
  if(subitemEditor){
    const rows=$('[data-project-subitem-rows]',subitemEditor),template=$('#project-subitem-template');
    const refresh=()=>{const items=$$('[data-project-subitem-row]',rows);$$('[data-project-subitem-empty]',rows).forEach(item=>item.remove());items.forEach((row,index)=>{const label=$('.subitem-index',row);if(label)label.textContent=String(index+1).padStart(2,'0')});if(!items.length){const empty=document.createElement('p');empty.className='empty-state-inline';empty.dataset.projectSubitemEmpty='';empty.textContent='尚未添加子项，可稍后完善。';rows.append(empty)}};
    $('[data-add-project-subitem]',subitemEditor)?.addEventListener('click',()=>{rows.append(template.content.cloneNode(true));refresh();$$('[data-project-subitem-row]',rows).at(-1)?.querySelector('input[name="subitem_name[]"]')?.focus()});
    rows.addEventListener('click',e=>{const button=e.target.closest('[data-remove-project-subitem]');if(!button)return;button.closest('[data-project-subitem-row]')?.remove();refresh()});
    refresh();
  }
  const forecastForm=$('[data-forecast-form]');if(forecastForm){const refresh=()=>{const total=$$('[data-forecast-hours]',forecastForm).reduce((s,i)=>s+(parseFloat(i.value)||0),0);const out=$('[data-forecast-total]');if(out)out.textContent=total.toFixed(1)};forecastForm.addEventListener('input',refresh);refresh()}

  $$('.trend-chart').forEach(canvas=>{
    let points=[];try{points=JSON.parse(canvas.dataset.points||'[]')}catch{}if(!points.length)return;
    const draw=()=>{
      const ratio=Math.min(window.devicePixelRatio||1,2),rect=canvas.getBoundingClientRect(),w=Math.max(rect.width,320),h=280;canvas.width=w*ratio;canvas.height=h*ratio;const c=canvas.getContext('2d');c.scale(ratio,ratio);c.clearRect(0,0,w,h);
      const pad={l:42,r:18,t:18,b:38},cw=w-pad.l-pad.r,ch=h-pad.t-pad.b,max=Math.max(40,...points.flatMap(p=>[p.actual_hours||0,p.forecast_hours||0,p.adjusted_hours||0]));
      c.font='11px Segoe UI';c.fillStyle='#7b8985';c.strokeStyle='#e6eae5';c.lineWidth=1;
      for(let i=0;i<=4;i++){const y=pad.t+ch*i/4;c.beginPath();c.moveTo(pad.l,y);c.lineTo(w-pad.r,y);c.stroke();c.fillText(String(Math.round(max*(1-i/4)))+'h',4,y+4)}
      const x=i=>pad.l+(points.length===1?cw/2:cw*i/(points.length-1)),y=v=>pad.t+ch-(v/max)*ch;
      const line=(key,color,dash=[])=>{c.beginPath();points.forEach((p,i)=>{const px=x(i),py=y(p[key]||0);i?c.lineTo(px,py):c.moveTo(px,py)});c.strokeStyle=color;c.lineWidth=2.5;c.setLineDash(dash);c.stroke();c.setLineDash([]);points.forEach((p,i)=>{c.beginPath();c.arc(x(i),y(p[key]||0),3.5,0,Math.PI*2);c.fillStyle=color;c.fill()})};
      line('forecast_hours','#a9beb7',[5,5]);line('actual_hours','#138b73');if(points.some(p=>p.has_adjusted))line('adjusted_hours','#d98b45',[3,3]);const labelStep=Math.max(1,Math.ceil(points.length/12));points.forEach((p,i)=>{if(i%labelStep!==0&&i!==points.length-1)return;c.fillStyle='#6d7a77';c.textAlign='center';c.fillText(p.week_label||'',x(i),h-14)});c.textAlign='left';
    };draw();let timer;window.addEventListener('resize',()=>{clearTimeout(timer);timer=setTimeout(draw,120)})
  });
  $$('.trend-chart').forEach((canvas,index)=>{
    const heading=canvas.closest('.panel')?.querySelector('.panel-heading');if(!heading||heading.querySelector('[data-export-png]'))return;
    const button=document.createElement('button');button.type='button';button.className='button mini';button.dataset.exportPng='';button.textContent='\u5bfc\u51fa PNG';
    button.addEventListener('click',()=>{const link=document.createElement('a');link.download=`workload-chart-${index+1}.png`;link.href=canvas.toDataURL('image/png');link.click()});heading.append(button);
  });})();
