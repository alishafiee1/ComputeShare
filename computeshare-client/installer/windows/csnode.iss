; Inno Setup script — node card 003. Build: iscc csnode.iss (unsigned: SmartScreen warning documented)
; Plan: docs/windows-installer-plan.md. Images: assets/ (rebuilt by assets/build_assets.py).
; Persian UI: put Farsi.isl (unofficial translation) into Inno Setup's Languages folder; this script then adds it.
#define Ver GetEnv("VERSION")
#if Ver == ""
  #error VERSION is not set (example: set VERSION=0.2.2 and run iscc csnode.iss)
#endif
; CI / client-only build: the three files below are only needed at COMPILE time (iscc checks they exist).
; In /CLIENTONLY mode they are never installed. So CI can compile with a small placeholder
; csnode-linux-rootfs.tar.gz and an "UNSIGNED TEST BUILD" SHA256SUMS.sig (see scripts/release.sh build_installer unsigned).
; Silent client-only install:  csnode-setup-X.exe /CLIENTONLY /VERYSILENT /SUPPRESSMSGBOXES /NORESTART /CURRENTUSER
#define HaveFa FileExists(CompilerPath + "Languages\Farsi.isl")

[Setup]
AppName=ComputeShare Node
AppVersion={#Ver}
AppPublisher=ComputeShare
DefaultDirName={autopf}\ComputeShare
DisableProgramGroupPage=yes
; admin by default (it can turn WSL on). If WSL is already installed, "just for me" works without admin:
; pick it in the start dialog, or pass /CURRENTUSER on the command line.
PrivilegesRequired=admin
PrivilegesRequiredOverridesAllowed=dialog commandline
UsedUserAreasWarning=no
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
Source: "csnode-linux-rootfs.tar.gz"; DestDir: "{app}"; Check: not ClientOnly
Source: "SHA256SUMS"; DestDir: "{app}"; Check: not ClientOnly
Source: "SHA256SUMS.sig"; DestDir: "{app}"; Check: not ClientOnly
Source: "assets\csnode.ico"; DestDir: "{app}"
Source: "assets\finish-panel.bmp"; Flags: dontcopy
Source: "assets\warning.bmp"; Flags: dontcopy
Source: "assets\restart.bmp"; Flags: dontcopy

[Registry]
Root: HKLM; Subkey: "SYSTEM\CurrentControlSet\Control\Session Manager\Environment"; ValueType: expandsz; ValueName: "Path"; ValueData: "{olddata};{app}"; Check: NeedsAddPathAdmin
Root: HKCU; Subkey: "Environment"; ValueType: expandsz; ValueName: "Path"; ValueData: "{olddata};{app}"; Check: NeedsAddPathUser

[UninstallRun]
Filename: "{app}\csnode.exe"; Parameters: "uninstall"; Flags: waituntilterminated; RunOnceId: "csnodepurge"; Check: not WasClientOnlyInstall

[UninstallDelete]
Type: files; Name: "{app}\client-only.flag"

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
en.BadServer=Server address must start with https:// (http:// only for 127.0.0.1 or localhost) and contain only letters, digits and . : / - _
en.BadToken=Join token is empty or has unusual characters.
en.BadName=Node name must be 3-32 characters: letters, digits or dash, starting with a letter.
en.WdTitle=Where to keep the Linux system
en.WdSub=The Linux system and the node's disk are big files. Pick a drive with free space.
en.WdIntro=The installer chose the drive with the most free space. You can change the folder. Use a local drive (not a USB stick or network drive), at least 15 GB free.
en.BadDrive=Pick a folder on a local fixed drive (not USB, not a network path), for example D:\ComputeShare.
en.BadDirChars=The folder path must not contain quotes or the characters % & | < > ^
en.LowSpace=This drive has less than 15 GB free. Pick another drive.
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
en.FinFailHead=Installation FAILED
en.FinFailWhy=ComputeShare was NOT installed correctly. The node is not working.
en.FinFailReason=Reason:
en.FinFailLog=Log file:
en.NeedAdminWsl=WSL is not installed on this computer, and turning it on needs administrator rights. Run this installer again and choose "all users" (or right-click, Run as administrator).
en.SilentBad=Silent install: invalid or missing parameter:
en.SilentNoAccept=Silent install: you must pass /ACCEPT to confirm that you accepted the terms (consent.txt).
en.NoSecretFile=Cannot read the file given for the secret value:
en.FinClientOk=ComputeShare client (cs) is installed. Open a NEW terminal and run: cs version
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
fa.WdTitle=جای نگهداری سیستم لینوکسی
fa.WdSub=سیستم لینوکسی و دیسک نود فایل‌های بزرگی هستند. درایوی را انتخاب کن که جای خالی دارد.
fa.WdIntro=نصاب درایوی را که بیشترین جای خالی دارد پیشنهاد داده. می‌توانی پوشه را عوض کنی. درایو داخلی انتخاب کن (نه فلش و نه شبکه) با دست‌کم ۱۵ گیگ جای خالی.
fa.BadDrive=پوشه‌ای روی یک درایو داخلی انتخاب کن (نه فلش، نه مسیر شبکه)، مثلاً D:\ComputeShare.
fa.BadDirChars=مسیر پوشه نباید گیومه یا نویسه‌های % & | < > ^ داشته باشد.
fa.LowSpace=این درایو کمتر از ۱۵ گیگ جای خالی دارد. درایو دیگری انتخاب کن.
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
fa.FinFailHead=نصب ناموفق بود
fa.FinFailWhy=ComputeShare درست نصب نشد و نود کار نمی‌کند.
fa.FinFailReason=دلیل:
fa.FinFailLog=فایل گزارش:
fa.NeedAdminWsl=WSL روی این کامپیوتر نصب نیست و روشن کردنش دسترسی ادمین می‌خواهد. نصاب را دوباره اجرا کن و «همهٔ کاربران» را انتخاب کن (یا راست‌کلیک و Run as administrator).
fa.SilentBad=نصب بی‌صدا: پارامتر نامعتبر یا جاافتاده:
fa.SilentNoAccept=نصب بی‌صدا: باید /ACCEPT بدهی تا قبول شرایط (consent.txt) تأیید شود.
fa.NoSecretFile=فایلِ مقدار محرمانه خوانده نشد:
fa.FinClientOk=کلاینت ComputeShare (دستور cs) نصب شد. یک ترمینال جدید باز کن و بزن: cs version
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

function GetDriveTypeW(lpRootPathName: String): Cardinal; external 'GetDriveTypeW@kernel32.dll stdcall';
procedure ExitProcess(uExitCode: Cardinal); external 'ExitProcess@kernel32.dll stdcall';

const
  { exit codes of a silent install (/VERYSILENT); 0 = success. Documented in INSTALL.md }
  CodeBadParam = 20;       { a command-line parameter is missing or invalid }
  CodeNoAccept = 21;       { /ACCEPT was not given }
  CodeSystem = 22;         { Windows too old, or virtualization is off }
  CodeNeedAdmin = 23;      { WSL is not installed and this run has no administrator rights }
  CodeInstallLinux = 30;   { WSL / Linux system install failed }
  CodeSetup = 31;          { node setup failed }
  CodeNameTaken = 32;      { node name already used on the server }
  CodeUnreachable = 33;    { server not reachable }
  CodeClientFiles = 24;    { /CLIENTONLY: cs.exe is missing after the copy }
  CodeReboot = 3010;       { WSL was just installed: restart Windows, then run the same command again }

var
  ReqPage: TWizardPage;
  WdPage: TInputDirWizardPage;
  AccPage, SpacePage: TInputQueryWizardPage;
  LblWin, LblVirt, LblWsl: TNewStaticText;
  WarnImg, RestartImg: TBitmapImage;
  WinOk, VirtOk, AdminOk, RebootNeeded, InstallFailed: Boolean;
  StatusText: String;
  FailReason, FailLog, LogDir: String;
  { the values of the questions: from the wizard pages, or from the command line when silent }
  ClientOnlyMode: Boolean;  { /CLIENTONLY: install only cs.exe + csnode.exe and the PATH entry; no WSL, no node setup }
  VServer, VToken, VName, VDir, VUser, VPass, VKey, VCpu, VMem, VDisk: String;

{ ---- command-line parameters (/SERVER=... style; see INSTALL.md) ---- }
function HasSwitch(Name: String): Boolean;
var i: Integer; a: String;
begin
  Result := False;
  for i := 1 to ParamCount do
  begin
    a := Uppercase(ParamStr(i));
    if (a = '/' + Name) or (Pos('/' + Name + '=', a) = 1) then Result := True;
  end;
end;

function Param(Name: String): String;
begin
  Result := Trim(ExpandConstant('{param:' + Name + '|}'));
end;

{ value of /NAME=... or, if absent, the first line of the file given in /NAMEFILE=... (keeps secrets off the command line) }
function SecretParam(Name: String; var Err: String): String;
var f: String; Raw: AnsiString; p, c: Integer;
begin
  Result := Param(Name);
  f := Param(Name + 'FILE');
  if (Result = '') and (f <> '') then
  begin
    if LoadStringFromFile(f, Raw) then
    begin
      { byte-wise: skip UTF-8 / UTF-16 BOMs and the NUL bytes of UTF-16 text, stop at the first line end }
      Result := '';
      for p := 1 to Length(Raw) do
      begin
        c := Ord(Raw[p]);
        if (c = 10) or (c = 13) then Break;
        if (c = 0) or (c = $EF) or (c = $BB) or (c = $BF) or (c = $FE) or (c = $FF) then
        begin
          { these bytes only appear in BOMs here; real secrets are ASCII }
          Continue;
        end;
        Result := Result + Chr(c);
      end;
      Result := Trim(Result);
    end else Err := CustomMessage('NoSecretFile') + ' /' + Name + 'FILE';
  end;
end;

function ClientOnly(): Boolean;
begin
  Result := ClientOnlyMode;
end;

{ uninstall: a client-only install has no Linux system to purge, so csnode uninstall is skipped }
function WasClientOnlyInstall(): Boolean;
begin
  Result := FileExists(ExpandConstant('{app}\client-only.flag'));
end;

function NeedsAddPathIn(Root: Integer; Sub: String): Boolean;
var p: string;
begin
  if not RegQueryStringValue(Root, Sub, 'Path', p) then begin Result := True; exit; end;
  Result := Pos(';' + ExpandConstant('{app}') + ';', ';' + p + ';') = 0;
end;

function NeedsAddPathAdmin(): Boolean;
begin
  Result := IsAdminInstallMode and NeedsAddPathIn(HKLM, 'SYSTEM\CurrentControlSet\Control\Session Manager\Environment');
end;

function NeedsAddPathUser(): Boolean;
begin
  Result := (not IsAdminInstallMode) and NeedsAddPathIn(HKCU, 'Environment');
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
  { plain http:// is accepted only for a broker on this same machine (127.0.0.1 or localhost), for local testing }
  if (not Result) and (Pos('http://', S) = 1) and AllChars(S, Letters + Digits + ':/.-_') then
  begin
    S := Copy(S, 8, Length(S));
    if (Pos('127.0.0.1', S) = 1) or (Pos('localhost', S) = 1) then
    begin
      S := Copy(S, 10, Length(S));  { both host names are 9 characters }
      Result := (S = '') or (S[1] = ':') or (S[1] = '/');
    end;
  end;
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

function BestDataDir(): String;
var
  c: Integer;
  Root: String;
  Free, Total, Best: Int64;
begin
  { the local fixed drive with the most free space; the system drive is the fallback }
  Result := ExpandConstant('{commonappdata}\csnode\wsl');
  Best := 0;
  for c := Ord('C') to Ord('Z') do
  begin
    Root := Chr(c) + ':\';
    if (GetDriveTypeW(Root) = 3) and GetSpaceOnDisk64(Root, Free, Total) and (Free > Best) then
    begin
      Best := Free;
      Result := Root + 'ComputeShare';
    end;
  end;
end;

function DataDirProblem(Dir: String): String;
var
  Root: String;
  Free, Total: Int64;
  i: Integer;
begin
  Result := '';
  Dir := Trim(Dir);
  for i := 1 to Length(Dir) do
    if Pos(Dir[i], '"%&|<>^') > 0 then begin Result := CustomMessage('BadDirChars'); Exit; end;
  if (Length(Dir) < 3) or (Dir[2] <> ':') or (Dir[3] <> '\') then begin Result := CustomMessage('BadDrive'); Exit; end;
  Root := Copy(Dir, 1, 3);
  if GetDriveTypeW(Root) <> 3 then begin Result := CustomMessage('BadDrive'); Exit; end;
  if GetSpaceOnDisk64(Root, Free, Total) and (Free < Int64(15) * 1024 * 1024 * 1024) then Result := CustomMessage('LowSpace');
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

function CheckWin(): Boolean;
var V: TWindowsVersion;
begin
  GetWindowsVersionEx(V);
  Result := V.Build >= 19044;
end;

function CheckVirt(): Boolean;
var rc: Integer;
begin
  Result := Exec(Sys64() + '\WindowsPowerShell\v1.0\powershell.exe',
    '-NoProfile -Command "if(((Get-CimInstance Win32_Processor).VirtualizationFirmwareEnabled -contains $true) -or (Get-CimInstance Win32_ComputerSystem).HypervisorPresent){exit 0}else{exit 1}"',
    '', SW_HIDE, ewWaitUntilTerminated, rc) and (rc = 0);
end;

function CheckWsl(): Boolean;
var rc: Integer;
begin
  Result := Exec(Sys64() + '\wsl.exe', '--status', '', SW_HIDE, ewWaitUntilTerminated, rc) and (rc = 0);
end;

procedure RunChecks();
var WslThere: Boolean;
begin
  WinOk := CheckWin();
  SetStatus(LblWin, WinOk, CustomMessage('WinOk'), CustomMessage('WinBad'));

  VirtOk := CheckVirt();
  SetStatus(LblVirt, VirtOk, CustomMessage('VirtOk'), CustomMessage('VirtBad'));

  WslThere := CheckWsl();
  { turning WSL on needs administrator rights; if WSL is already there, a per-user install is enough }
  AdminOk := WslThere or IsAdminInstallMode;
  if WslThere then SetStatus(LblWsl, True, CustomMessage('WslOk'), '')
  else if AdminOk then begin SetStatus(LblWsl, True, '', ''); LblWsl.Caption := '[i]  ' + CustomMessage('WslNew'); end
  else SetStatus(LblWsl, False, '', CustomMessage('NeedAdminWsl'));

  WarnImg.Visible := not (WinOk and VirtOk and AdminOk);
end;

{ one place for every failure: log + reason; silent = exit with a non-zero code, wizard = red finish page }
procedure Fatal(Code: Integer; Msg, LogFile: String);
begin
  FailReason := Msg;
  FailLog := LogFile;
  Log('FAILED (' + IntToStr(Code) + '): ' + Msg + ' ' + LogFile);
  SaveStringToFile(LogDir + '\install-result.log', 'FAILED code=' + IntToStr(Code) + #13#10 + Msg + #13#10 + LogFile + #13#10, False);
  if WizardSilent then ExitProcess(Code);
  InstallFailed := True;
  SuppressibleMsgBox(Msg + #13#10 + LogFile, mbError, MB_OK, IDOK);
end;

function ValidLimit(S: String): Boolean;
begin
  Result := (S = '') or (AllChars(S, Digits + '.GiBMKTgibmkt') and (Pos(S[1], Digits) > 0));
end;

{ silent install: read every answer from the command line, validate it, check the system; exit non-zero with a log on any problem }
procedure BadParam(What: String);
begin
  Fatal(CodeBadParam, CustomMessage('SilentBad') + ' ' + What, '');
end;

procedure SilentChecks();
var Err: String;
begin
  if ClientOnlyMode then Exit;  { nothing to ask or check: no WSL, no node, no consent for a node }
  if not HasSwitch('ACCEPT') then begin Fatal(CodeNoAccept, CustomMessage('SilentNoAccept'), ''); Exit; end;
  Err := '';
  { TODO: the default server https://node.n3xus.ir is being dropped (domain going away). Change it here AND in InitializeWizard when the new address is decided. }
  VServer := Param('SERVER'); if VServer = '' then VServer := 'https://node.n3xus.ir';
  VToken := SecretParam('TOKEN', Err);
  VName := Param('NODENAME');
  VDir := Param('WSLDIR'); if VDir = '' then VDir := BestDataDir();
  VUser := Param('SPACEUSER'); if VUser = '' then VUser := 'user';
  VPass := SecretParam('SPACEPASS', Err);
  VKey := Param('SPACEKEY');
  if Err <> '' then begin Fatal(CodeBadParam, Err, ''); Exit; end;
  if not ValidServer(VServer) then BadParam('/SERVER')
  else if not ValidToken(VToken) then BadParam('/TOKEN or /TOKENFILE')
  else if not ValidName(VName) then BadParam('/NODENAME')
  else if DataDirProblem(VDir) <> '' then BadParam('/WSLDIR - ' + DataDirProblem(VDir))
  else if not ValidUser(VUser) then BadParam('/SPACEUSER')
  else if (VPass <> '') and ((Length(VPass) < 12) or not IsAscii(VPass)) then BadParam('/SPACEPASS (12+ ASCII characters)')
  else if not ValidKey(VKey) then BadParam('/SPACEKEY')
  else if (VPass = '') and (VKey = '') then BadParam('/SPACEPASS or /SPACEKEY (one of them is required)')
  else if not ValidLimit(VCpu) then BadParam('/CPU')
  else if not ValidLimit(VMem) then BadParam('/MEMORY')
  else if not ValidLimit(VDisk) then BadParam('/DISK');

  if not CheckWin() then Fatal(CodeSystem, CustomMessage('WinBad'), '')
  else if not CheckVirt() then Fatal(CodeSystem, CustomMessage('VirtBad'), '')
  else if (not CheckWsl()) and (not IsAdminInstallMode) then Fatal(CodeNeedAdmin, CustomMessage('NeedAdminWsl'), '');
end;

function InitializeSetup(): Boolean;
begin
  Result := True;
  LogDir := ExpandConstant('{localappdata}\ComputeShare\logs');
  ForceDirectories(LogDir);
  DeleteFile(LogDir + '\install-result.log');
  ClientOnlyMode := HasSwitch('CLIENTONLY');
  VCpu := Param('CPU');   { empty = the suggested safe value }
  VMem := Param('MEMORY');
  VDisk := Param('DISK');
  if WizardSilent then SilentChecks();
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
  { page 3b: where the Linux system file lives (the installer asks the drive and the folder) }
  WdPage := CreateInputDirPage(wpSelectDir, CustomMessage('WdTitle'), CustomMessage('WdSub'), CustomMessage('WdIntro'), False, 'ComputeShare');
  WdPage.Add('');
  WdPage.Values[0] := BestDataDir();

  AccPage := CreateInputQueryPage(WdPage.ID, CustomMessage('AccTitle'), CustomMessage('AccSub'), CustomMessage('AccIntro'));
  AccPage.Add(CustomMessage('Server'), False);
  AccPage.Add(CustomMessage('Token'), True);
  AccPage.Add(CustomMessage('NodeName'), False);
  { TODO: same default as in SilentChecks (domain is being dropped) }
  AccPage.Values[0] := 'https://node.n3xus.ir';
  { command-line values prefill the pages (and are the answers when the install is silent) }
  if Param('SERVER') <> '' then AccPage.Values[0] := Param('SERVER');
  if Param('NODENAME') <> '' then AccPage.Values[2] := Param('NODENAME');
  if Param('WSLDIR') <> '' then WdPage.Values[0] := Param('WSLDIR');

  { page 5: guest space }
  SpacePage := CreateInputQueryPage(AccPage.ID, CustomMessage('SpTitle'), CustomMessage('SpSub'), CustomMessage('SpIntro'));
  SpacePage.Add(CustomMessage('SpUser'), False);
  SpacePage.Add(CustomMessage('SpPass'), True);
  SpacePage.Add(CustomMessage('SpPass2'), True);
  SpacePage.Add(CustomMessage('SpKey'), False);
  SpacePage.Values[0] := 'user';
  if Param('SPACEUSER') <> '' then SpacePage.Values[0] := Param('SPACEUSER');

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
    begin
      { must not look like the normal success page }
      WizardForm.FinishedHeadingLabel.Caption := CustomMessage('FinFailHead');
      WizardForm.FinishedHeadingLabel.Font.Color := $002B39C0;  { red }
      WizardForm.FinishedLabel.Caption := CustomMessage('FinFailWhy') + #13#10#13#10 + CustomMessage('FinFailReason') + #13#10 + FailReason + #13#10#13#10 +
        CustomMessage('FinFailLog') + #13#10 + FailLog + #13#10#13#10 + CustomMessage('FinFail');
    end else if ClientOnlyMode then
      WizardForm.FinishedLabel.Caption := CustomMessage('FinClientOk')
    else
      WizardForm.FinishedLabel.Caption := CustomMessage('FinOk') + #13#10#13#10 + CustomMessage('FinLimits') + #13#10#13#10 + StatusText;
  end;
end;

function Reject(Msg: String): Boolean;
begin
  { silent: never a dialog; log and exit with the bad-parameter code }
  if WizardSilent then Fatal(CodeBadParam, Msg, '');
  SuppressibleMsgBox(Msg, mbError, MB_OK, IDOK);
  Result := False;
end;

{ silent installs take every answer from the command line (SilentChecks); the wizard pages are never used }
function ShouldSkipPage(PageID: Integer): Boolean;
begin
  Result := (WizardSilent or ClientOnlyMode) and ((PageID = ReqPage.ID) or (PageID = WdPage.ID) or (PageID = AccPage.ID) or (PageID = SpacePage.ID));
end;

function NextButtonClick(CurPageID: Integer): Boolean;
begin
  Result := True;
  if WizardSilent or ClientOnlyMode then Exit;
  if CurPageID = ReqPage.ID then
  begin
    if not (WinOk and VirtOk and AdminOk) then Result := Reject(CustomMessage('ReqFail'));
  end
  else if CurPageID = WdPage.ID then
  begin
    if DataDirProblem(WdPage.Values[0]) <> '' then Result := Reject(DataDirProblem(WdPage.Values[0]));
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

procedure CollectValues();
begin
  if WizardSilent or ClientOnlyMode then Exit;  { silent: SilentChecks already filled them from the command line }
  VServer := Trim(AccPage.Values[0]);
  VToken := Trim(AccPage.Values[1]);
  VName := Trim(AccPage.Values[2]);
  VDir := Trim(WdPage.Values[0]);
  VUser := Trim(SpacePage.Values[0]);
  VPass := SpacePage.Values[1];
  VKey := Trim(SpacePage.Values[3]);
end;

procedure DoInstall();
var
  App, Exe, InstLog, SetupLog, PwFile, Line: String;
  rc: Integer;
  Raw: AnsiString;
begin
  App := ExpandConstant('{app}');
  Exe := '"' + App + '\csnode.exe"';
  InstLog := LogDir + '\install.log';
  SetupLog := LogDir + '\setup.log';
  PwFile := ExpandConstant('{tmp}\cspw.txt');
  if ClientOnlyMode then
  begin
    { client only: cs.exe / csnode.exe and the PATH entry are already done by the normal install; just verify and mark }
    if not FileExists(App + '\cs.exe') then begin Fatal(CodeClientFiles, 'cs.exe', App); Exit; end;
    SaveStringToFile(App + '\client-only.flag', '1' + #13#10, False);
    SaveStringToFile(LogDir + '\install-result.log', 'OK client-only' + #13#10, False);
    Exit;
  end;
  CollectValues();

  WizardForm.StatusLabel.Caption := CustomMessage('StInstall');
  rc := RunCmd(Exe + ' install --rootfs "' + App + '\csnode-linux-rootfs.tar.gz" --wsl-dir "' + VDir + '"', InstLog);
  if rc = ExitReboot then
  begin
    RebootNeeded := True;
    SaveStringToFile(LogDir + '\install-result.log', 'RESTART NEEDED code=' + IntToStr(CodeReboot) + #13#10, False);
    if WizardSilent then ExitProcess(CodeReboot);
    Exit;
  end;
  if rc <> 0 then begin Fatal(CodeInstallLinux, CustomMessage('FailInstall'), InstLog); Exit; end;

  WizardForm.StatusLabel.Caption := CustomMessage('StSetup');
  Line := Exe + ' setup --yes --safe-defaults --server "' + VServer + '" --name ' + VName +
          ' --user ' + VUser + ' --token "' + VToken + '"';
  if VKey <> '' then Line := Line + ' --key "' + VKey + '"';
  if VCpu <> '' then Line := Line + ' --cpu ' + VCpu;
  if VMem <> '' then Line := Line + ' --memory ' + VMem;
  if VDisk <> '' then Line := Line + ' --disk ' + VDisk;
  if VPass <> '' then
  begin
    SaveStringToFile(PwFile, VPass + #13#10, False);
    Line := Line + ' --password-stdin < "' + PwFile + '"';  { the password never goes on a command line }
  end;
  rc := RunCmd(Line, SetupLog);
  DeleteFile(PwFile);
  if rc = ExitNameTaken then begin Fatal(CodeNameTaken, CustomMessage('FailName'), SetupLog); Exit; end;
  if rc = ExitUnreachable then begin Fatal(CodeUnreachable, CustomMessage('FailServer'), SetupLog); Exit; end;
  if rc <> 0 then begin Fatal(CodeSetup, CustomMessage('FailSetup'), SetupLog); Exit; end;

  SaveStringToFile(LogDir + '\install-result.log', 'OK' + #13#10, False);
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
