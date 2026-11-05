; Inno Setup script — node card 003. Build: iscc csnode.iss (unsigned: SmartScreen warning documented)
; Plan: docs/windows-installer-plan.md. Images: assets/ (rebuilt by assets/build_assets.py).
; Persian UI: put Farsi.isl (unofficial translation) into Inno Setup's Languages folder; this script then adds it.
#define Ver GetEnv("VERSION")
#if Ver == ""
  #error VERSION is not set (example: set VERSION=0.2.2 and run iscc csnode.iss)
#endif
#define HaveFa FileExists(CompilerPath + "Languages\Farsi.isl")

[Setup]
AppName=ComputeShare Node
AppVersion={#Ver}
AppPublisher=ComputeShare
DefaultDirName={autopf}\ComputeShare
DisableProgramGroupPage=yes
PrivilegesRequired=admin
OutputBaseFilename=csnode-setup-{#Ver}
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0.19044
WizardStyle=modern
; solid + maximum compression ran out of memory on a normal laptop; this is lighter and still small
Compression=lzma2/normal
SolidCompression=no
SetupIconFile=assets\csnode.ico
UninstallDisplayIcon={app}\csnode.ico
LicenseFile=consent.txt
WizardImageFile=assets\welcome-panel.bmp,assets\welcome-panel-200.bmp
WizardSmallImageFile=assets\small-logo.bmp,assets\small-logo-200.bmp

[Languages]
Name: "en"; MessagesFile: "compiler:Default.isl"
#if HaveFa
Name: "fa"; MessagesFile: "compiler:Languages\Farsi.isl"
#endif

[Files]
Source: "..\..\dist\csnode-windows-amd64.exe"; DestDir: "{app}"; DestName: "csnode.exe"
Source: "..\..\dist\cs-windows-amd64.exe"; DestDir: "{app}"; DestName: "cs.exe"
Source: "csnode-linux-rootfs.tar.gz"; DestDir: "{app}"
Source: "SHA256SUMS"; DestDir: "{app}"
Source: "SHA256SUMS.sig"; DestDir: "{app}"
Source: "assets\csnode.ico"; DestDir: "{app}"
Source: "assets\finish-panel.bmp"; Flags: dontcopy
Source: "assets\warning.bmp"; Flags: dontcopy
Source: "assets\restart.bmp"; Flags: dontcopy

[Registry]
Root: HKLM; Subkey: "SYSTEM\CurrentControlSet\Control\Session Manager\Environment"; ValueType: expandsz; ValueName: "Path"; ValueData: "{olddata};{app}"; Check: NeedsAddPath

[UninstallRun]
Filename: "{app}\csnode.exe"; Parameters: "uninstall"; Flags: waituntilterminated; RunOnceId: "csnodepurge"

[CustomMessages]
en.ReqTitle=System check
en.ReqSub=ComputeShare needs Windows 10 (21H2) or newer and virtualization turned on.
en.WinOk=Windows version is supported
en.WinBad=Windows 10 version 21H2 (build 19044) or newer is required
en.VirtOk=Virtualization is turned on
en.VirtBad=Virtualization is OFF in the BIOS/UEFI. Turn on "Intel VT-x" or "AMD-V / SVM" in the BIOS, then run this installer again
en.WslOk=WSL2 is already installed
en.WslNew=WSL2 will be installed now (Windows may ask for a restart)
en.ReqFail=Fix the red items first, then run the installer again.
en.AccTitle=Your node
en.AccSub=Connect this computer to a ComputeShare server.
en.AccIntro=Enter the server address, the join token you received, and a name for your node.
en.Server=Server address:
en.Token=Join token:
en.NodeName=Node name (letters, digits, dash; 3-32 characters, starts with a letter):
en.BadServer=Server address must start with https:// and contain only letters, digits and . : / - _
en.BadToken=Join token is empty or has unusual characters.
en.BadName=Node name must be 3-32 characters: letters, digits or dash, starting with a letter.
en.SpTitle=Guest space
en.SpSub=The space is where people you trust will log in.
en.SpIntro=Choose the space user. Give a password (12+ characters), or only a public key, or both.
en.SpUser=Space user name:
en.SpPass=Password (12+ characters, leave empty for key only):
en.SpPass2=Repeat the password:
en.SpKey=Public key (optional, one line starting with ssh-):
en.BadUser=User name: lowercase letters, digits, dash or underscore, starting with a letter or underscore.
en.BadPass=The password must be at least 12 characters.
en.BadPassAscii=Use only English letters, digits and symbols in the password (no Persian letters or spaces at the edges).
en.PassMismatch=The two passwords are not the same.
en.NeedPassOrKey=Give a password or a public key (or both).
en.BadKey=The public key must be one line starting with ssh- and must not contain quotes or special characters.
en.StInstall=Installing WSL and the Linux system... this can take several minutes.
en.StSetup=Creating the space and connecting to the server...
en.FailInstall=The Linux system could not be installed. Details are in:
en.FailSetup=The node could not be set up. Details are in:
en.FailName=This node name is already taken on the server. Run the installer again with another name.
en.FailServer=The server could not be reached. Check the address and your internet, then run the installer again.
en.FinOk=ComputeShare is ready. Others can now connect to your node.
en.FinRestart=Windows must restart to finish enabling WSL2. After the restart, run this installer again to finish.
en.FinFail=The installation did not finish. See the message shown earlier and the log file.
en.FinLimits=CPU, memory and disk limits use safe suggested values. Change them later in the app.
#if HaveFa
fa.ReqTitle=بررسی سیستم
fa.ReqSub=ComputeShare ویندوز ۱۰ (نسخهٔ 21H2) یا جدیدتر و مجازی‌سازی روشن می‌خواهد.
fa.WinOk=نسخهٔ ویندوز پشتیبانی می‌شود
fa.WinBad=ویندوز ۱۰ نسخهٔ 21H2 (build 19044) یا جدیدتر لازم است
fa.VirtOk=مجازی‌سازی روشن است
fa.VirtBad=مجازی‌سازی در BIOS خاموش است. گزینهٔ Intel VT-x یا AMD-V/SVM را در BIOS روشن کن و دوباره نصاب را اجرا کن
fa.WslOk=WSL2 از قبل نصب است
fa.WslNew=WSL2 همین حالا نصب می‌شود (ممکن است ویندوز ری‌استارت بخواهد)
fa.ReqFail=اول موارد قرمز را درست کن و دوباره نصاب را اجرا کن.
fa.AccTitle=نود تو
fa.AccSub=این کامپیوتر را به یک سرور ComputeShare وصل کن.
fa.AccIntro=آدرس سرور، توکن عضویتی که گرفته‌ای و یک نام برای نودت را وارد کن.
fa.Server=آدرس سرور:
fa.Token=توکن عضویت:
fa.NodeName=نام نود (حرف، رقم، خط تیره؛ ۳ تا ۳۲ نویسه، شروع با حرف):
fa.BadServer=آدرس سرور باید با https:// شروع شود و فقط حرف، رقم و . : / - _ داشته باشد.
fa.BadToken=توکن خالی است یا نویسهٔ عجیب دارد.
fa.BadName=نام نود باید ۳ تا ۳۲ نویسه باشد: حرف، رقم یا خط تیره، و با حرف شروع شود.
fa.SpTitle=فضای مهمان
fa.SpSub=فضا جاییه که آدم‌های مورد اعتمادت وارد می‌شوند.
fa.SpIntro=نام کاربر فضا را انتخاب کن. رمز (حداقل ۱۲ نویسه) یا فقط کلید عمومی یا هر دو بده.
fa.SpUser=نام کاربر فضا:
fa.SpPass=رمز (حداقل ۱۲ نویسه، برای فقط‌کلید خالی بگذار):
fa.SpPass2=تکرار رمز:
fa.SpKey=کلید عمومی (اختیاری، یک خط که با ssh- شروع می‌شود):
fa.BadUser=نام کاربر: حرف کوچک انگلیسی، رقم، خط تیره یا زیرخط، و شروع با حرف یا زیرخط.
fa.BadPass=رمز باید دست‌کم ۱۲ نویسه باشد.
fa.BadPassAscii=در رمز فقط حرف انگلیسی، رقم و نماد بگذار (حرف فارسی نه).
fa.PassMismatch=دو رمز یکی نیستند.
fa.NeedPassOrKey=رمز یا کلید عمومی (یا هر دو) بده.
fa.BadKey=کلید عمومی باید یک خط باشد که با ssh- شروع می‌شود و گیومه یا نویسهٔ خاص ندارد.
fa.StInstall=در حال نصب WSL و سیستم لینوکسی... چند دقیقه طول می‌کشد.
fa.StSetup=در حال ساخت فضا و اتصال به سرور...
fa.FailInstall=سیستم لینوکسی نصب نشد. جزئیات این‌جاست:
fa.FailSetup=نود راه‌اندازی نشد. جزئیات این‌جاست:
fa.FailName=این نام نود روی سرور گرفته شده. نصاب را با نام دیگری دوباره اجرا کن.
fa.FailServer=به سرور وصل نشد. آدرس و اینترنت را چک کن و نصاب را دوباره اجرا کن.
fa.FinOk=ComputeShare آماده است. حالا بقیه می‌توانند به نودت وصل شوند.
fa.FinRestart=ویندوز باید ری‌استارت شود تا WSL2 کامل روشن شود. بعد از ری‌استارت همین نصاب را دوباره اجرا کن.
fa.FinFail=نصب کامل نشد. پیام قبلی و فایل گزارش را ببین.
fa.FinLimits=سقف پردازنده، رم و دیسک روی مقدارهای امن پیشنهادی است. بعداً داخل برنامه عوضش کن.
#endif

[Code]
const
  ExitReboot = 10;      { csnode.exe install: WSL was just installed, restart first }
  ExitUnreachable = 3;  { csnode setup: server not reachable }
  ExitNameTaken = 4;    { csnode setup: node name already used }
  LettersLower = 'abcdefghijklmnopqrstuvwxyz';
  Letters = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ';
  Digits = '0123456789';

var
  ReqPage: TWizardPage;
  AccPage, SpacePage: TInputQueryWizardPage;
  LblWin, LblVirt, LblWsl: TNewStaticText;
  WarnImg, RestartImg: TBitmapImage;
  WinOk, VirtOk, RebootNeeded, InstallFailed: Boolean;
  StatusText: String;

function NeedsAddPath(): Boolean;
var p: string;
begin
  if not RegQueryStringValue(HKLM, 'SYSTEM\CurrentControlSet\Control\Session Manager\Environment', 'Path', p) then begin Result := True; exit; end;
  Result := Pos(';' + ExpandConstant('{app}') + ';', ';' + p + ';') = 0;
end;

function Sys64(): String;
begin
  { the installer is 32-bit; powershell.exe and wsl.exe live in the 64-bit system folder }
  if IsWin64 then Result := ExpandConstant('{sysnative}') else Result := ExpandConstant('{sys}');
end;

function AllChars(S, Allowed: String): Boolean;
var i: Integer;
begin
  Result := S <> '';
  for i := 1 to Length(S) do
    if Pos(S[i], Allowed) = 0 then begin Result := False; Exit; end;
end;

function ValidName(S: String): Boolean;
begin
  Result := (Length(S) >= 3) and (Length(S) <= 32) and AllChars(S, Letters + Digits + '-') and (Pos(S[1], Letters) > 0);
end;

function ValidUser(S: String): Boolean;
begin
  Result := (Length(S) >= 1) and (Length(S) <= 32) and AllChars(S, LettersLower + Digits + '-_') and (Pos(S[1], LettersLower + '_') > 0);
end;

function ValidToken(S: String): Boolean;
begin
  Result := (Length(S) >= 6) and AllChars(S, Letters + Digits + '._-');
end;

function ValidServer(S: String): Boolean;
begin
  Result := (Pos('https://', S) = 1) and (Length(S) > 12) and AllChars(S, Letters + Digits + ':/.-_');
end;

function IsAscii(S: String): Boolean;
var i: Integer;
begin
  Result := True;
  for i := 1 to Length(S) do
    if (Ord(S[i]) < 32) or (Ord(S[i]) > 126) then Result := False;
end;

function ValidKey(S: String): Boolean;
var i: Integer;
begin
  Result := True;
  if S = '' then Exit;
  Result := Pos('ssh-', S) = 1;
  for i := 1 to Length(S) do
    if Pos(S[i], '"%&|<>^' + #13 + #10) > 0 then Result := False;
end;

function MakeLabel(Page: TWizardPage; Top: Integer): TNewStaticText;
begin
  Result := TNewStaticText.Create(Page);
  Result.Parent := Page.Surface;
  Result.Left := 0;
  Result.Top := Top;
  Result.Width := Page.SurfaceWidth;
  Result.AutoSize := False;
  Result.WordWrap := True;
  Result.Height := ScaleY(34);
end;

procedure SetStatus(L: TNewStaticText; Ok: Boolean; Good, Bad: String);
begin
  if Ok then
  begin
    L.Font.Color := $003C8A1E;  { green #1e8a3c }
    L.Caption := '[OK]  ' + Good;
  end else
  begin
    L.Font.Color := $002B39C0;  { red #c0392b }
    L.Caption := '[X]  ' + Bad;
  end;
end;

procedure RunChecks();
var
  V: TWindowsVersion;
  rc: Integer;
  WslThere: Boolean;
begin
  GetWindowsVersionEx(V);
  WinOk := V.Build >= 19044;
  SetStatus(LblWin, WinOk, CustomMessage('WinOk'), CustomMessage('WinBad'));

  VirtOk := Exec(Sys64() + '\WindowsPowerShell\v1.0\powershell.exe',
    '-NoProfile -Command "if(((Get-CimInstance Win32_Processor).VirtualizationFirmwareEnabled -contains $true) -or (Get-CimInstance Win32_ComputerSystem).HypervisorPresent){exit 0}else{exit 1}"',
    '', SW_HIDE, ewWaitUntilTerminated, rc) and (rc = 0);
  SetStatus(LblVirt, VirtOk, CustomMessage('VirtOk'), CustomMessage('VirtBad'));

  WslThere := Exec(Sys64() + '\wsl.exe', '--status', '', SW_HIDE, ewWaitUntilTerminated, rc) and (rc = 0);
  SetStatus(LblWsl, True, '', '');
  if WslThere then LblWsl.Caption := '[OK]  ' + CustomMessage('WslOk')
  else LblWsl.Caption := '[i]  ' + CustomMessage('WslNew');

  WarnImg.Visible := not (WinOk and VirtOk);
end;

procedure InitializeWizard();
begin
  ExtractTemporaryFile('finish-panel.bmp');
  ExtractTemporaryFile('warning.bmp');
  ExtractTemporaryFile('restart.bmp');
  WizardForm.WizardBitmapImage2.Bitmap.LoadFromFile(ExpandConstant('{tmp}\finish-panel.bmp'));

  { page 2: system check }
  ReqPage := CreateCustomPage(wpWelcome, CustomMessage('ReqTitle'), CustomMessage('ReqSub'));
  LblWin := MakeLabel(ReqPage, 0);
  LblVirt := MakeLabel(ReqPage, ScaleY(40));
  LblWsl := MakeLabel(ReqPage, ScaleY(80));
  WarnImg := TBitmapImage.Create(ReqPage);
  WarnImg.Parent := ReqPage.Surface;
  WarnImg.AutoSize := True;
  WarnImg.Bitmap.LoadFromFile(ExpandConstant('{tmp}\warning.bmp'));
  WarnImg.Left := 0;
  WarnImg.Top := ScaleY(125);
  WarnImg.Visible := False;

  { page 4: account of the node }
  AccPage := CreateInputQueryPage(wpSelectDir, CustomMessage('AccTitle'), CustomMessage('AccSub'), CustomMessage('AccIntro'));
  AccPage.Add(CustomMessage('Server'), False);
  AccPage.Add(CustomMessage('Token'), True);
  AccPage.Add(CustomMessage('NodeName'), False);
  AccPage.Values[0] := 'https://node.n3xus.ir';

  { page 5: guest space }
  SpacePage := CreateInputQueryPage(AccPage.ID, CustomMessage('SpTitle'), CustomMessage('SpSub'), CustomMessage('SpIntro'));
  SpacePage.Add(CustomMessage('SpUser'), False);
  SpacePage.Add(CustomMessage('SpPass'), True);
  SpacePage.Add(CustomMessage('SpPass2'), True);
  SpacePage.Add(CustomMessage('SpKey'), False);
  SpacePage.Values[0] := 'user';

  RestartImg := TBitmapImage.Create(WizardForm);
  RestartImg.Parent := WizardForm.FinishedPage;
  RestartImg.AutoSize := True;
  RestartImg.Bitmap.LoadFromFile(ExpandConstant('{tmp}\restart.bmp'));
  RestartImg.Visible := False;
end;

procedure CurPageChanged(CurPageID: Integer);
begin
  if CurPageID = ReqPage.ID then
    RunChecks()
  else if CurPageID = wpFinished then
  begin
    if RebootNeeded then
    begin
      WizardForm.FinishedLabel.Caption := CustomMessage('FinRestart');
      RestartImg.Left := WizardForm.FinishedLabel.Left;
      RestartImg.Top := WizardForm.FinishedLabel.Top + WizardForm.FinishedLabel.Height + ScaleY(12);
      RestartImg.Visible := True;
    end else if InstallFailed then
      WizardForm.FinishedLabel.Caption := CustomMessage('FinFail')
    else
      WizardForm.FinishedLabel.Caption := CustomMessage('FinOk') + #13#10#13#10 + CustomMessage('FinLimits') + #13#10#13#10 + StatusText;
  end;
end;

function Reject(Msg: String): Boolean;
begin
  MsgBox(Msg, mbError, MB_OK);
  Result := False;
end;

function NextButtonClick(CurPageID: Integer): Boolean;
begin
  Result := True;
  if CurPageID = ReqPage.ID then
  begin
    if not (WinOk and VirtOk) then Result := Reject(CustomMessage('ReqFail'));
  end
  else if CurPageID = AccPage.ID then
  begin
    if not ValidServer(Trim(AccPage.Values[0])) then Result := Reject(CustomMessage('BadServer'))
    else if not ValidToken(Trim(AccPage.Values[1])) then Result := Reject(CustomMessage('BadToken'))
    else if not ValidName(Trim(AccPage.Values[2])) then Result := Reject(CustomMessage('BadName'));
  end
  else if CurPageID = SpacePage.ID then
  begin
    if not ValidUser(Trim(SpacePage.Values[0])) then Result := Reject(CustomMessage('BadUser'))
    else if (SpacePage.Values[1] <> '') and (Length(SpacePage.Values[1]) < 12) then Result := Reject(CustomMessage('BadPass'))
    else if not IsAscii(SpacePage.Values[1]) then Result := Reject(CustomMessage('BadPassAscii'))
    else if SpacePage.Values[1] <> SpacePage.Values[2] then Result := Reject(CustomMessage('PassMismatch'))
    else if (SpacePage.Values[1] = '') and (Trim(SpacePage.Values[3]) = '') then Result := Reject(CustomMessage('NeedPassOrKey'))
    else if not ValidKey(Trim(SpacePage.Values[3])) then Result := Reject(CustomMessage('BadKey'));
  end;
end;

function RunCmd(Line, LogFile: String): Integer;
var rc: Integer;
begin
  { one cmd.exe line; the double quote after /C keeps the inner quotes intact }
  if not Exec(ExpandConstant('{cmd}'), '/C "' + Line + ' > "' + LogFile + '" 2>&1"', '', SW_HIDE, ewWaitUntilTerminated, rc) then rc := -1;
  Result := rc;
end;

procedure FailWith(Msg, LogFile: String);
begin
  InstallFailed := True;
  MsgBox(Msg + #13#10 + LogFile, mbError, MB_OK);
end;

procedure DoInstall();
var
  App, Exe, InstLog, SetupLog, PwFile, Line, Token: String;
  rc: Integer;
  Raw: AnsiString;
begin
  App := ExpandConstant('{app}');
  Exe := '"' + App + '\csnode.exe"';
  InstLog := App + '\install.log';
  SetupLog := App + '\setup.log';
  PwFile := ExpandConstant('{tmp}\cspw.txt');

  WizardForm.StatusLabel.Caption := CustomMessage('StInstall');
  rc := RunCmd(Exe + ' install --rootfs "' + App + '\csnode-linux-rootfs.tar.gz"', InstLog);
  if rc = ExitReboot then begin RebootNeeded := True; Exit; end;
  if rc <> 0 then begin FailWith(CustomMessage('FailInstall'), InstLog); Exit; end;

  WizardForm.StatusLabel.Caption := CustomMessage('StSetup');
  Token := Trim(AccPage.Values[1]);
  Line := Exe + ' setup --yes --safe-defaults --server "' + Trim(AccPage.Values[0]) + '" --name ' + Trim(AccPage.Values[2]) +
          ' --user ' + Trim(SpacePage.Values[0]) + ' --token "' + Token + '"';
  if Trim(SpacePage.Values[3]) <> '' then Line := Line + ' --key "' + Trim(SpacePage.Values[3]) + '"';
  if SpacePage.Values[1] <> '' then
  begin
    SaveStringToFile(PwFile, SpacePage.Values[1] + #13#10, False);
    Line := Line + ' --password-stdin < "' + PwFile + '"';  { the password never goes on a command line }
  end;
  rc := RunCmd(Line, SetupLog);
  DeleteFile(PwFile);
  if rc = ExitNameTaken then begin FailWith(CustomMessage('FailName'), SetupLog); Exit; end;
  if rc = ExitUnreachable then begin FailWith(CustomMessage('FailServer'), SetupLog); Exit; end;
  if rc <> 0 then begin FailWith(CustomMessage('FailSetup'), SetupLog); Exit; end;

  { show the connection line (address and fingerprint) on the last page }
  RunCmd(Exe + ' status', ExpandConstant('{tmp}\status.txt'));
  if LoadStringFromFile(ExpandConstant('{tmp}\status.txt'), Raw) then StatusText := String(Raw);
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then DoInstall();
end;

function NeedRestart(): Boolean;
begin
  Result := RebootNeeded;
end;
