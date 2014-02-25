package main

import (
    "flag"
    "fmt"
    "io"
    "log"
    "os"
    "os/exec"
    "path/filepath"
    "regexp"
    "runtime"
    "strconv"
    "strings"
    "time"
)

const (
    ENVNAME = "BACKUPDIR"
)

var (
    verbose   *bool
    dryrun    *bool
    firsttime  bool
)

type BackupFile struct {
    src     string
    ref     string
    dst     string
    changed bool
}

func Input(question string) string {
    fmt.Print(question)
    var answer string
    fmt.Scanln(&answer)
    return answer
}

func Copy(src, dst string) error{
    if *dryrun {
        return nil
    }
    sf, err := os.Open(src)
    if err != nil {
        return err
    }
    defer sf.Close()
    df, err := os.Create(dst)
    if err != nil {
        return err
    }
    defer df.Close()
    io.Copy(df,sf)
    if *verbose {
        fmt.Printf("C:FROM: %s -> TO: %s\n", src, dst)
    }
    return nil
}


func HardLink(src, dst string) error {
    if *dryrun {
        return nil
    }
    _, err := os.Stat(src)
    if err != nil {
        return err
    }
    os.Remove(dst)
    cmd := exec.Command("cmd", "/c", "mklink", "/H", dst, src)
    _, err = cmd.CombinedOutput()
    if *verbose {
        fmt.Printf("H:FROM: %s -> TO: %s\n", src, dst)
    }
    if err != nil {
        return err
    }
    return nil
}

func Backup(bf *BackupFile) {
    dir, _ := filepath.Split(bf.dst)
    var err error
    if !*dryrun {
        err = os.MkdirAll(dir, os.ModeDir)
    }
    if bf.ref =="" || bf.changed {
        err = Copy(bf.src, bf.dst)
        if err != nil {
            return
        }
    } else {
        err = HardLink(bf.ref, bf.dst)
        if err != nil {
            return
        }
    }
    return
}

func GoBackup(ch chan *BackupFile, done chan int) {
    for {
        bf := <-ch
        if bf == nil {
            break
        }
        Backup(bf)
    }
    done <- 1
}

func ListDir(filename string) ([]string, error) {
    f, err := os.Open(filename)
    if err != nil {
        return nil, err
    }
    defer f.Close()
    files, err := f.Readdirnames(-1)
    if err != nil {
        return nil, err
    }
    return files, err
}

func LastDir(dir string) (string, error) {
    re := regexp.MustCompile("\\d{8}_\\d{4}")
    list, err := ListDir(dir)
    if err != nil {
        return "", err
    }
    lastday := 0
    lastminute := 0
    for _, fn := range list {
        if re.MatchString(fn) {
            dm := strings.Split(fn, "_")
            day, err := strconv.Atoi(dm[0])
            minute, err := strconv.Atoi(dm[1])
            if err != nil {
                return "", err
            }
            if day>lastday {
                lastday = day
                lastminute = minute
            } else if day==lastday {
                if minute>lastminute {
                    lastminute = minute
                }
            }
        }
    }
    if lastday == 0 && lastminute == 0 {
        return "", nil
    } else {
        return fmt.Sprintf("%08d_%04d",lastday,lastminute), nil
    }
}

func main() {
    runtime.GOMAXPROCS(2)
    drive   := flag.String("d", "F", "Drive Letter")
    user    := flag.String("u", "fukushima", "User Name")
    Recurse := flag.Bool("r", true, "Recursive")
    verbose  = flag.Bool("v", false, "Verbose")
    source  := flag.String("s", "", "Source")
    dryrun   = flag.Bool("n", false, "Dry Running")
    flag.Parse()

    var OrgPath []string
    folder := filepath.Join(fmt.Sprintf("%s:",*drive), "Users", *user)
    if *source == "" {
        env := os.Getenv(ENVNAME)
        if env == "" {
            fmt.Printf("Set %s\n",ENVNAME)
            return
        }
        OrgPath = strings.Split(env,";")
    } else {
        OrgPath = strings.Split(*source,";")
    }
    now := time.Now()

    for _, org := range OrgPath {
        fn := filepath.Join(folder,filepath.Base(org))
        last, err := LastDir(fn)
        RefPath := filepath.Join(fn, last)
        NewPath := filepath.Join(fn, now.Format("20060102_1504"))
        if err != nil || last == "" {
            firsttime = true
            fmt.Printf("It's first time to backup: %s\n", fn)
            fmt.Printf("Backup\n    FROM: %s\n    TO  : %s\n", org, NewPath)
        } else {
            fmt.Printf("Backup\n    FROM: %s\n    TO  : %s\n   (REF : %s)\n", org, NewPath, RefPath)
        }
        if !*dryrun {
            err = os.MkdirAll(NewPath, os.ModeDir)
            if err != nil {
                fmt.Println(err)
                return
            }
        }
        start := time.Now()
        ch := make(chan *BackupFile)
        done := make(chan int)
        go GoBackup(ch, done)
        walkFn := func(path string, info os.FileInfo, err error) error {
            stat, err := os.Stat(path)
            if err != nil {
                return err
            }
            if stat.IsDir() && path != org && !*Recurse {
                fmt.Println("skipping dir: ", path)
                return filepath.SkipDir
            }
            if !stat.IsDir() {
                dst := strings.Replace(path, org, NewPath, -1)
                if firsttime {
                    ch <- &BackupFile{path, "", dst, false}
                    return nil
                } else {
                    ref := strings.Replace(path, org, RefPath, -1)
                    refstat, err := os.Stat(ref)
                    if err != nil {
                        ch <- &BackupFile{path, "", dst, false}
                        return nil
                    }
                    ch <- &BackupFile{path, ref, dst, stat.ModTime().Local().After(refstat.ModTime().Local())}
                }
            }
            return nil
        }
        err = filepath.Walk(org, walkFn)
        if err != nil {
            log.Fatal(err)
        }
        ch <- nil
        end := time.Now()
        fmt.Printf("%fsec\n", (end.Sub(start)).Seconds())
        <-done
    }
}
